// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	if attempted == 0 {
		return "skipped"
	}
	return attemptedPrimaryOutcome(succeeded, failed)
}

func attemptedPrimaryOutcome(succeeded, failed int) string {
	if succeeded > 0 && failed == 0 {
		return "success"
	}
	if succeeded > 0 && failed > 0 {
		return "partial"
	}
	if succeeded == 0 && failed == 0 {
		return "empty"
	}
	return "failed"
}

func normalizeTrigger(trigger Trigger) string {
	if trigger == "" {
		return string(TriggerOnFailures)
	}
	return string(trigger)
}
