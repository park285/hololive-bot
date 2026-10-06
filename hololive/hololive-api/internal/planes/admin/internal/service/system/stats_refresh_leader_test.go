package system

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestCollector_GetCurrentStats_CanceledLeaderDoesNotCacheUnavailableRemote(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64

		collector := &Collector{
			httpClient: &http.Client{Transport: statsRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if requests.Add(1) == 1 {
					<-req.Context().Done()

					return nil, req.Context().Err()
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"goroutines":42}`)),
				}, nil
			})},
			endpoints:   []ServiceEndpoint{{Name: "remote", URL: "http://stats.test/health"}},
			cacheTTL:    time.Hour,
			serviceName: defaultLocalServiceName,
		}
		leaderCtx, cancel := context.WithCancel(t.Context())

		defer cancel()

		leaderResult := make(chan statsRefreshResult, 1)

		go func() {
			stats, err := collector.GetCurrentStats(leaderCtx)
			leaderResult <- statsRefreshResult{stats: stats, err: err}
		}()

		synctest.Wait()

		if got := requests.Load(); got != 1 {
			t.Fatalf("started refresh requests=%d want=1", got)
		}

		waiterResult := make(chan statsRefreshResult, 1)

		go func() {
			stats, err := collector.GetCurrentStats(t.Context())
			waiterResult <- statsRefreshResult{stats: stats, err: err}
		}()

		synctest.Wait()
		cancel()

		if got := <-leaderResult; got.stats != nil || !errors.Is(got.err, context.Canceled) {
			t.Errorf("canceled leader=(%+v, %v) want=(nil, canceled)", got.stats, got.err)
		}

		got := <-waiterResult
		if got.err != nil || got.stats == nil {
			t.Fatalf("healthy waiter=(%+v, %v)", got.stats, got.err)
		}

		if requests.Load() != 2 || !got.stats.ServiceGoroutines[1].Available || got.stats.ServiceGoroutines[1].Goroutines != 42 {
			t.Fatalf("canceled refresh poisoned cache: requests=%d stats=%+v", requests.Load(), got.stats)
		}
	})
}
