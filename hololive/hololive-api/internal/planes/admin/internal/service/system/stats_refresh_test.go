package system

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/shirou/gopsutil/v4/common"
)

type statsRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f statsRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type statsRefreshResult struct {
	stats *SystemStats
	err   error
}

type statsGatedWaitContext struct {
	done    <-chan struct{}
	err     func() error
	entered chan struct{}
	ready   <-chan struct{}
}

func (*statsGatedWaitContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *statsGatedWaitContext) Done() <-chan struct{} {
	close(ctx.entered)
	<-ctx.ready

	return ctx.done
}

func (ctx *statsGatedWaitContext) Err() error {
	return ctx.err()
}

func (*statsGatedWaitContext) Value(any) any {
	return nil
}

func newBlockedStatsCollector(release <-chan struct{}, requests *atomic.Int64) *Collector {
	return &Collector{
		httpClient: &http.Client{
			Transport: statsRoundTripperFunc(func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				<-release

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"goroutines":42}`)),
				}, nil
			}),
		},
		endpoints:   []ServiceEndpoint{{Name: "remote", URL: "http://stats.test/health"}},
		cacheTTL:    time.Hour,
		serviceName: defaultLocalServiceName,
	}
}

func TestCollector_GetCurrentStats_CanceledWaiterReturnsBeforeRefresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{name: "cancel", want: context.Canceled},
		{name: "deadline", want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testCanceledStatsWaiter(t, tc.want)
			})
		})
	}
}

func testCanceledStatsWaiter(t *testing.T, want error) {
	t.Helper()

	release := make(chan struct{})
	finishRefresh := sync.OnceFunc(func() { close(release) })

	defer finishRefresh()

	var requests atomic.Int64

	collector := newBlockedStatsCollector(release, &requests)
	leaderResult := make(chan statsRefreshResult, 1)

	go func() {
		stats, err := collector.GetCurrentStats(t.Context())
		leaderResult <- statsRefreshResult{stats: stats, err: err}
	}()

	synctest.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("started refresh requests=%d want=1", got)
	}

	waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	waiterResult := make(chan statsRefreshResult, 1)

	go func() {
		stats, err := collector.GetCurrentStats(waitCtx)
		waiterResult <- statsRefreshResult{stats: stats, err: err}
	}()

	synctest.Wait()

	if errors.Is(want, context.Canceled) {
		cancel()
	} else {
		time.Sleep(time.Second)
	}

	synctest.Wait()

	select {
	case got := <-waiterResult:
		if !errors.Is(got.err, want) || got.stats != nil {
			t.Errorf("waiter result=(%+v, %v) want=(nil, %v)", got.stats, got.err, want)
		}
	default:
		t.Error("canceled waiter still waits for blocked refresh")
	}

	select {
	case got := <-leaderResult:
		t.Fatalf("waiter cancellation finished blocked refresh: %+v", got)
	default:
	}

	finishRefresh()

	if got := <-leaderResult; got.err != nil || got.stats == nil {
		t.Fatalf("leader result=(%+v, %v) want successful stats", got.stats, got.err)
	}

	if got := requests.Load(); got != 1 {
		t.Fatalf("refresh requests=%d want=1", got)
	}
}

func TestCollector_GetCurrentStats_LiveWaitersShareRefreshAndReceiveClones(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finishRefresh := sync.OnceFunc(func() { close(release) })

		defer finishRefresh()

		var requests atomic.Int64

		collector := newBlockedStatsCollector(release, &requests)

		const callers = 6

		results := make(chan statsRefreshResult, callers)

		for range callers {
			go func() {
				stats, err := collector.GetCurrentStats(t.Context())
				results <- statsRefreshResult{stats: stats, err: err}
			}()
		}

		synctest.Wait()

		if got := requests.Load(); got != 1 {
			t.Fatalf("concurrent refresh requests=%d want=1", got)
		}

		finishRefresh()

		allStats := make([]*SystemStats, 0, callers)

		for range callers {
			got := <-results
			if got.err != nil || got.stats == nil {
				t.Fatalf("caller result=(%+v, %v) want successful stats", got.stats, got.err)
			}

			allStats = append(allStats, got.stats)
		}

		assertStatsClonesIndependent(t, collector, allStats)

		if got := requests.Load(); got != 1 {
			t.Fatalf("refresh requests after cache hit=%d want=1", got)
		}
	})
}

func assertStatsClonesIndependent(t *testing.T, collector *Collector, allStats []*SystemStats) {
	t.Helper()

	for _, stats := range allStats[1:] {
		if stats == allStats[0] || !reflect.DeepEqual(stats, allStats[0]) {
			t.Fatal("callers did not receive independent copies of the same snapshot")
		}
	}

	allStats[0].CPUUsage = -1
	allStats[0].ServiceGoroutines[1].Goroutines = -1

	for _, stats := range allStats[1:] {
		if stats.CPUUsage == -1 || stats.ServiceGoroutines[1].Goroutines != 42 {
			t.Fatal("mutating one caller's stats changed another caller's stats")
		}
	}

	cached, err := collector.GetCurrentStats(t.Context())
	if err != nil || cached == nil {
		t.Fatalf("cache hit result=(%+v, %v) want successful stats", cached, err)
	}

	if cached.CPUUsage == -1 || cached.ServiceGoroutines[1].Goroutines != 42 {
		t.Fatal("mutating caller's stats changed the cache")
	}
}

func TestCollector_GetCurrentStats_CancellationWinsWhenRefreshAlsoCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finishRefresh := sync.OnceFunc(func() { close(release) })

		defer finishRefresh()

		var requests atomic.Int64

		collector := newBlockedStatsCollector(release, &requests)
		leaderResult := make(chan statsRefreshResult, 1)

		go func() {
			stats, err := collector.GetCurrentStats(t.Context())
			leaderResult <- statsRefreshResult{stats: stats, err: err}
		}()

		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		ready := make(chan struct{})
		allowWait := sync.OnceFunc(func() { close(ready) })

		defer allowWait()

		waitCtx := &statsGatedWaitContext{done: ctx.Done(), err: ctx.Err, entered: make(chan struct{}), ready: ready}
		waiterResult := make(chan statsRefreshResult, 1)

		go func() {
			stats, err := collector.GetCurrentStats(waitCtx)
			waiterResult <- statsRefreshResult{stats: stats, err: err}
		}()

		<-waitCtx.entered
		cancel()
		finishRefresh()

		if got := <-leaderResult; got.err != nil || got.stats == nil {
			t.Fatalf("leader result=(%+v, %v) want successful stats", got.stats, got.err)
		}

		allowWait()

		if got := <-waiterResult; got.stats != nil || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled waiter result=(%+v, %v) want=(nil, canceled)", got.stats, got.err)
		}

		cached, err := collector.GetCurrentStats(ctx)
		if err != nil || cached == nil {
			t.Fatalf("immediate cache hit with canceled context=(%+v, %v) want successful cached stats", cached, err)
		}
	})
}

func TestCollector_GetCurrentStats_FailedRefreshDoesNotPublishOrBlockNextCaller(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux proc memory statistics")
	}

	collector := NewCollector(nil, sharedh3.ClientOptions{})
	stale := &SystemStats{CPUUsage: -1}
	staleAt := time.Now().Add(-time.Hour)

	collector.cached = stale
	collector.cachedAt = staleAt

	failedCtx := context.WithValue(t.Context(), common.EnvKey, common.EnvMap{
		common.HostProcEnvKey: t.TempDir(),
	})

	stats, err := collector.GetCurrentStats(failedCtx)
	if stats != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed refresh=(%+v, %v) want=(nil, wrapped missing proc file)", stats, err)
	}

	if !strings.HasPrefix(err.Error(), "collect current stats: failed to get memory stats: ") {
		t.Fatalf("collection error context changed: %v", err)
	}

	if collector.cached != stale || !collector.cachedAt.Equal(staleAt) {
		t.Fatal("failed refresh published a cache value")
	}

	stats, err = collector.GetCurrentStats(t.Context())
	if err != nil || stats == nil || stats.CPUUsage == -1 {
		t.Fatalf("next refresh=(%+v, %v) want fresh successful stats", stats, err)
	}

	if !collector.cachedAt.After(staleAt) {
		t.Fatal("successful next refresh did not update cache timestamp")
	}
}
