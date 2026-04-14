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

package dispatch

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	dispatcherMetricsInitOnce sync.Once

	dispatcherRetryScheduled       prometheus.Counter
	dispatcherRetryDLQMoved        *prometheus.CounterVec
	dispatcherRetryBudgetExhausted prometheus.Counter
	dispatcherRetryAttempt         prometheus.Histogram
	dispatcherRetryBackoff         prometheus.Histogram
)

func initDispatcherMetrics() {
	dispatcherMetricsInitOnce.Do(func() {
		dispatcherRetryScheduled = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatcher_retry_scheduled_total",
				Help: "Total alarm dispatcher envelopes scheduled for durable retry.",
			},
		)

		dispatcherRetryDLQMoved = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatcher_dlq_moved_total",
				Help: "Total alarm dispatcher envelopes moved to DLQ by reason.",
			},
			[]string{"reason"},
		)

		dispatcherRetryBudgetExhausted = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatcher_retry_budget_exhausted_total",
				Help: "Total alarm dispatcher envelopes that exhausted their retry budget.",
			},
		)

		dispatcherRetryAttempt = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_dispatcher_retry_attempt",
				Help:    "Alarm dispatcher retry attempt number observed on send failure.",
				Buckets: []float64{1, 2, 3, 4, 5, 8, 13},
			},
		)

		dispatcherRetryBackoff = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_dispatcher_retry_backoff_seconds",
				Help:    "Alarm dispatcher retry backoff duration in seconds.",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600},
			},
		)
	})
}
