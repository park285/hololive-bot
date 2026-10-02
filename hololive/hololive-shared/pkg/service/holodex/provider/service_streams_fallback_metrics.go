package holodexprovider

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 공식 일정 fallback의 metric 이름과 label은 범용 실행기(internal/service/fallback)를 합치기 전과 같다.
// 이제 service label은 holodex 하나뿐이고, trigger는 "primary가 비었고 실패한 org가 있음" 하나뿐이다.
const (
	streamFallbackServiceLabel = "holodex"
	streamFallbackTriggerLabel = "on_empty_primary_with_error"

	streamFallbackOutcomeSkipped = "skipped"
	streamFallbackOutcomeError   = "error"
	streamFallbackOutcomeHit     = "hit"
	streamFallbackOutcomeMiss    = "miss"
)

var (
	streamFallbackMetricsOnce    sync.Once
	streamFallbackPrimaryTotal   *prometheus.CounterVec
	streamFallbackExecutionTotal *prometheus.CounterVec
)

func initStreamFallbackMetrics() {
	streamFallbackMetricsOnce.Do(func() {
		streamFallbackPrimaryTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_primary_total",
				Help: "Total primary phase outcomes before fallback decisions.",
			},
			[]string{"service", "operation", "outcome"},
		)

		streamFallbackExecutionTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_execution_total",
				Help: "Total fallback execution outcomes by service, operation, and trigger.",
			},
			[]string{"service", "operation", "trigger", "outcome"},
		)
	})
}

// observeStreamPrimary는 primary 단계 결과를 한 번 기록한다. 호출자 취소는 실패가 아니라 canceled로 센다.
func observeStreamPrimary(operation string, result orgFetchResult) {
	initStreamFallbackMetrics()
	streamFallbackPrimaryTotal.WithLabelValues(streamFallbackServiceLabel, operation, streamPrimaryOutcome(result)).Inc()
}

func observeStreamFallbackExecution(operation, outcome string) {
	initStreamFallbackMetrics()
	streamFallbackExecutionTotal.WithLabelValues(streamFallbackServiceLabel, operation, streamFallbackTriggerLabel, outcome).Inc()
}

func streamPrimaryOutcome(result orgFetchResult) string {
	failed := len(result.Failed)

	switch {
	case result.Attempted == 0:
		return "skipped"
	case len(result.Canceled) > 0:
		return "canceled"
	case result.Succeeded > 0 && failed == 0:
		return "success"
	case result.Succeeded > 0:
		return "partial"
	case failed == 0:
		return "empty"
	default:
		return "failed"
	}
}
