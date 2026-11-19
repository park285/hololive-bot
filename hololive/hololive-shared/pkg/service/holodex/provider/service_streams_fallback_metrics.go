package holodexprovider

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

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
