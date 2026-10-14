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

// newReleasableStatsCollector의 원격 조회는 release가 닫힐 때까지 기다리고, 그 전에 요청 ctx가 취소되면 취소 오류로 끝난다.
func newReleasableStatsCollector(release <-chan struct{}, requests *atomic.Int64) *Collector {
	return &Collector{
		httpClient: &http.Client{Transport: statsRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-release:
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
}

func getCurrentStatsInBackground(ctx context.Context, collector *Collector) <-chan statsRefreshResult {
	result := make(chan statsRefreshResult, 1)

	go func() {
		stats, err := collector.GetCurrentStats(ctx)
		result <- statsRefreshResult{stats: stats, err: err}
	}()

	return result
}

// 먼저 들어온 요청이 취소돼도 공유 수집의 원격 조회는 취소되지 않는다. 그래서 취소가 원격 장애처럼 캐시되지 않고,
// 남은 대기자와 다음 호출은 같은 한 번의 수집 결과를 받는다.
func TestCollector_GetCurrentStats_CanceledLeaderDoesNotCancelSharedRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64

		release := make(chan struct{})
		collector := newReleasableStatsCollector(release, &requests)
		leaderCtx, cancel := context.WithCancel(t.Context())

		defer cancel()

		leaderResult := getCurrentStatsInBackground(leaderCtx, collector)

		synctest.Wait()

		waiterResult := getCurrentStatsInBackground(t.Context(), collector)

		synctest.Wait()
		cancel()

		if got := <-leaderResult; got.stats != nil || !errors.Is(got.err, context.Canceled) {
			t.Errorf("canceled leader=(%+v, %v) want=(nil, canceled)", got.stats, got.err)
		}

		close(release)

		got := <-waiterResult
		if got.err != nil || got.stats == nil || !got.stats.ServiceGoroutines[1].Available || got.stats.ServiceGoroutines[1].Goroutines != 42 {
			t.Fatalf("waiter after leader cancellation=(%+v, %v), want healthy remote stats", got.stats, got.err)
		}

		cached, err := collector.GetCurrentStats(t.Context())
		if err != nil || cached == nil || !cached.ServiceGoroutines[1].Available {
			t.Fatalf("cache hit after shared refresh=(%+v, %v)", cached, err)
		}

		if got := requests.Load(); got != 1 {
			t.Fatalf("remote requests=%d want=1 shared refresh", got)
		}
	})
}
