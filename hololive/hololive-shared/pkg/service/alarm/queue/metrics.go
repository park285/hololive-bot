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

package queue

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	queueMetricsInitOnce sync.Once

	alarmQueueDrainDuration  prometheus.Histogram
	alarmQueueDrainBatch     prometheus.Histogram
	alarmQueueDrainTotal     *prometheus.CounterVec
	alarmQueueEnvelopeTotal  *prometheus.CounterVec
	alarmQueueClaimReleased  prometheus.Counter
	alarmQueueRetryScheduled prometheus.Counter
	alarmQueueRetryDrained   prometheus.Counter
	alarmQueueDLQMoved       prometheus.Counter
)

func initQueueMetrics() {
	queueMetricsInitOnce.Do(func() {
		alarmQueueDrainDuration = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_dispatch_queue_drain_duration_seconds",
				Help:    "Alarm dispatch queue drain duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		)

		alarmQueueDrainBatch = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_dispatch_queue_drain_batch_size",
				Help:    "Alarm dispatch queue drained envelope count per batch.",
				Buckets: []float64{0, 1, 2, 5, 10, 20, 50, 100},
			},
		)

		alarmQueueDrainTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_drain_total",
				Help: "Total alarm dispatch queue drain attempts by result.",
			},
			[]string{"result"},
		)

		alarmQueueEnvelopeTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_envelopes_total",
				Help: "Total parsed alarm dispatch queue envelopes by result.",
			},
			[]string{"result"},
		)

		alarmQueueClaimReleased = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_claim_keys_released_total",
				Help: "Total released alarm dispatch queue claim keys.",
			},
		)

		alarmQueueRetryScheduled = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_retry_scheduled_total",
				Help: "Total alarm dispatch queue envelopes scheduled into the delayed retry queue.",
			},
		)

		alarmQueueRetryDrained = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_retry_drained_total",
				Help: "Total delayed retry envelopes drained into active processing.",
			},
		)

		alarmQueueDLQMoved = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_dispatch_queue_dlq_moved_total",
				Help: "Total alarm dispatch queue envelopes moved to the dead-letter queue.",
			},
		)
	})
}
