package holodexprovider

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 공식 일정 fallback metric의 service label은 holodex로, trigger label은 "primary가 비었고 실패한 org가 있음"을 뜻하는
// on_empty_primary_with_error로 고정한다.
const (
	streamFallbackServiceLabel = "holodex"
	streamFallbackTriggerLabel = "on_empty_primary_with_error"

	streamFallbackOutcomeSkipped = "skipped"
	streamFallbackOutcomeError   = "error"
	streamFallbackOutcomeHit     = "hit"
	streamFallbackOutcomeMiss    = "miss"
)

type streamFallbackMetrics struct {
	primaryTotal   *prometheus.CounterVec
	executionTotal *prometheus.CounterVec
}

// 기본 registry에는 한 번만 등록하므로 첫 관측 때 묶음으로 만든다.
var streamFallbackMetricsFor = sync.OnceValue(func() *streamFallbackMetrics {
	return &streamFallbackMetrics{
		primaryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_primary_total",
				Help: "Total primary phase outcomes before fallback decisions.",
			},
			[]string{"service", "operation", "outcome"},
		),
		executionTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_execution_total",
				Help: "Total fallback execution outcomes by service, operation, and trigger.",
			},
			[]string{"service", "operation", "trigger", "outcome"},
		),
	}
})

// observeStreamPrimary는 primary 단계 결과를 한 번 기록한다. 호출자 취소는 실패가 아니라 canceled로 센다.
func observeStreamPrimary(operation string, result orgFetchResult) {
	streamFallbackMetricsFor().primaryTotal.WithLabelValues(streamFallbackServiceLabel, operation, streamPrimaryOutcome(result)).Inc()
}

func observeStreamFallbackExecution(operation, outcome string) {
	streamFallbackMetricsFor().executionTotal.WithLabelValues(streamFallbackServiceLabel, operation, streamFallbackTriggerLabel, outcome).Inc()
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
