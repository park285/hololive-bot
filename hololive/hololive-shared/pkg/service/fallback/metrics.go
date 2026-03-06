package fallback

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	fallbackMetricsInitOnce sync.Once
	fallbackPrimaryTotal    *prometheus.CounterVec
	fallbackExecutionTotal  *prometheus.CounterVec
)

func initFallbackMetrics() {
	fallbackMetricsInitOnce.Do(func() {
		fallbackPrimaryTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_primary_total",
				Help: "Total primary phase outcomes before fallback decisions.",
			},
			[]string{"service", "operation", "outcome"},
		)

		fallbackExecutionTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_fallback_execution_total",
				Help: "Total fallback execution outcomes by service, operation, and trigger.",
			},
			[]string{"service", "operation", "trigger", "outcome"},
		)
	})
}

func ObservePrimaryPhase(service, operation string, attempted, succeeded, failed int) {
	initFallbackMetrics()
	fallbackPrimaryTotal.WithLabelValues(service, operation, primaryOutcome(attempted, succeeded, failed)).Inc()
}

func ObserveExecution(service, operation string, trigger Trigger, outcome string) {
	initFallbackMetrics()
	fallbackExecutionTotal.WithLabelValues(service, operation, normalizeTrigger(trigger), outcome).Inc()
}

func primaryOutcome(attempted, succeeded, failed int) string {
	switch {
	case attempted == 0:
		return "skipped"
	case succeeded > 0 && failed == 0:
		return "success"
	case succeeded > 0 && failed > 0:
		return "partial"
	case succeeded == 0 && failed == 0:
		return "empty"
	default:
		return "failed"
	}
}

func normalizeTrigger(trigger Trigger) string {
	if trigger == "" {
		return string(TriggerOnFailures)
	}
	return string(trigger)
}
