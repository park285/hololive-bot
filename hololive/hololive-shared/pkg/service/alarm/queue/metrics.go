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
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	queueMetricsInitOnce sync.Once

	alarmQueueDrainDuration               prometheus.Histogram
	alarmQueueDrainBatch                  prometheus.Histogram
	alarmQueueDrainTotal                  *prometheus.CounterVec
	alarmQueueEnvelopeTotal               *prometheus.CounterVec
	alarmQueueClaimReleased               prometheus.Counter
	alarmQueueRetryScheduled              prometheus.Counter
	alarmQueueRetryDrained                prometheus.Counter
	alarmQueueDLQMoved                    prometheus.Counter
	alarmDispatchPublishBatchDuration     prometheus.Histogram
	alarmDispatchPublishRequestedTotal    *prometheus.CounterVec
	alarmDispatchPublishProcessedTotal    *prometheus.CounterVec
	alarmDispatchPublishInsertedTotal     *prometheus.CounterVec
	alarmDispatchPublishDuplicateTotal    *prometheus.CounterVec
	alarmDispatchPublishHashConflictTotal *prometheus.CounterVec
	alarmDispatchWakeupSentTotal          prometheus.Counter
	alarmDispatchWakeupSuppressedTotal    prometheus.Counter
	alarmDispatchWakeupFailedTotal        prometheus.Counter
	alarmDispatchWakeupExpireFailedTotal  prometheus.Counter
)

func initQueueMetrics() {
	queueMetricsInitOnce.Do(func() {
		initAlarmQueueDrainMetrics()
		initAlarmDispatchPublishMetrics()
		initAlarmDispatchWakeupMetrics()
	})
}

func initAlarmQueueDrainMetrics() {
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
	alarmQueueClaimReleased = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_dispatch_queue_claim_keys_released_total",
		Help: "Total released alarm dispatch queue claim keys.",
	})
	alarmQueueRetryScheduled = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_dispatch_queue_retry_scheduled_total",
		Help: "Total alarm dispatch queue envelopes scheduled into the delayed retry queue.",
	})
	alarmQueueRetryDrained = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_dispatch_queue_retry_drained_total",
		Help: "Total delayed retry envelopes drained into active processing.",
	})
	alarmQueueDLQMoved = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_dispatch_queue_dlq_moved_total",
		Help: "Total alarm dispatch queue envelopes moved to the dead-letter queue.",
	})
}

func initAlarmDispatchPublishMetrics() {
	alarmDispatchPublishBatchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "alarm_dispatch_publish_batch_duration_seconds",
			Help:    "Alarm dispatch publisher batch duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)
	alarmDispatchPublishRequestedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_requested_deliveries_total",
			Help: "Requested alarm dispatch deliveries published.",
		},
		[]string{"mode"},
	)
	alarmDispatchPublishProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_processed_deliveries_total",
			Help: "Alarm dispatch deliveries successfully processed by the active publish mode.",
		},
		[]string{"mode"},
	)
	alarmDispatchPublishInsertedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_inserted_deliveries_total",
			Help: "Inserted alarm dispatch deliveries.",
		},
		[]string{"mode"},
	)
	alarmDispatchPublishDuplicateTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_duplicate_deliveries_total",
			Help: "Duplicate alarm dispatch deliveries skipped.",
		},
		[]string{"mode"},
	)
	alarmDispatchPublishHashConflictTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_hash_conflict_total",
			Help: "Alarm dispatch event hash conflicts observed while publishing.",
		},
		[]string{"mode"},
	)
}

func initAlarmDispatchWakeupMetrics() {
	alarmDispatchWakeupSentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_wakeup_sent_total",
		Help: "Alarm dispatch wakeup tokens sent.",
	})
	alarmDispatchWakeupSuppressedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_wakeup_suppressed_total",
		Help: "Alarm dispatch wakeup tokens suppressed by guard.",
	})
	alarmDispatchWakeupFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_wakeup_failed_total",
		Help: "Alarm dispatch wakeup token send failures.",
	})
	alarmDispatchWakeupExpireFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_wakeup_expire_failed_total",
		Help: "Alarm dispatch wakeup list TTL failures.",
	})
}

func observeAlarmDispatchPublishBatch(duration time.Duration, mode PublishMode, result dispatchoutbox.PublishBatchResult) {
	initQueueMetrics()
	modeLabel := string(normalizePublishMode(mode))
	alarmDispatchPublishBatchDuration.Observe(duration.Seconds())
	alarmDispatchPublishRequestedTotal.WithLabelValues(modeLabel).Add(float64(result.RequestedDeliveries))
	alarmDispatchPublishProcessedTotal.WithLabelValues(modeLabel).Add(float64(result.ProcessedDeliveries))
	alarmDispatchPublishInsertedTotal.WithLabelValues(modeLabel).Add(float64(result.InsertedDeliveries))
	alarmDispatchPublishDuplicateTotal.WithLabelValues(modeLabel).Add(float64(result.DuplicateDeliveries))
	alarmDispatchPublishHashConflictTotal.WithLabelValues(modeLabel).Add(float64(result.HashConflictEvents))
}
