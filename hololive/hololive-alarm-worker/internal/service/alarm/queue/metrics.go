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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

var (
	queueMetricsInitOnce sync.Once

	alarmDispatchPublishBatchDuration     prometheus.Histogram
	alarmDispatchPublishRequestedTotal    prometheus.Counter
	alarmDispatchPublishProcessedTotal    prometheus.Counter
	alarmDispatchPublishInsertedTotal     prometheus.Counter
	alarmDispatchPublishDuplicateTotal    prometheus.Counter
	alarmDispatchPublishHashConflictTotal prometheus.Counter
	alarmDispatchWakeupSentTotal          prometheus.Counter
	alarmDispatchWakeupSuppressedTotal    prometheus.Counter
	alarmDispatchWakeupFailedTotal        prometheus.Counter
	alarmDispatchWakeupExpireFailedTotal  prometheus.Counter
)

func initQueueMetrics() {
	queueMetricsInitOnce.Do(func() {
		initAlarmDispatchPublishMetrics()
		initAlarmDispatchWakeupMetrics()
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
	alarmDispatchPublishRequestedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_publish_requested_deliveries_total",
		Help: "Requested alarm dispatch deliveries published.",
	})
	alarmDispatchPublishProcessedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_publish_processed_deliveries_total",
		Help: "Alarm dispatch deliveries successfully processed by the active publish mode.",
	})
	alarmDispatchPublishInsertedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_publish_inserted_deliveries_total",
		Help: "Inserted alarm dispatch deliveries.",
	})
	alarmDispatchPublishDuplicateTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_publish_duplicate_deliveries_total",
		Help: "Duplicate alarm dispatch deliveries skipped.",
	})
	alarmDispatchPublishHashConflictTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_publish_hash_conflict_total",
		Help: "Alarm dispatch event hash conflicts observed while publishing.",
	})
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

func observeAlarmDispatchPublishBatch(duration time.Duration, result *dispatchoutbox.PublishBatchResult) {
	initQueueMetrics()

	if alarmDispatchPublishBatchDuration == nil ||
		alarmDispatchPublishRequestedTotal == nil ||
		alarmDispatchPublishProcessedTotal == nil ||
		alarmDispatchPublishInsertedTotal == nil ||
		alarmDispatchPublishDuplicateTotal == nil ||
		alarmDispatchPublishHashConflictTotal == nil {
		return
	}

	alarmDispatchPublishBatchDuration.Observe(duration.Seconds())
	alarmDispatchPublishRequestedTotal.Add(float64(result.RequestedDeliveries))
	alarmDispatchPublishProcessedTotal.Add(float64(result.ProcessedDeliveries))
	alarmDispatchPublishInsertedTotal.Add(float64(result.InsertedDeliveries))
	alarmDispatchPublishDuplicateTotal.Add(float64(result.DuplicateDeliveries))
	alarmDispatchPublishHashConflictTotal.Add(float64(result.HashConflictEvents))
}

func observeAlarmDispatchWakeupSent() {
	initQueueMetrics()

	if alarmDispatchWakeupSentTotal == nil {
		return
	}

	alarmDispatchWakeupSentTotal.Inc()
}

func observeAlarmDispatchWakeupSuppressed() {
	initQueueMetrics()

	if alarmDispatchWakeupSuppressedTotal == nil {
		return
	}

	alarmDispatchWakeupSuppressedTotal.Inc()
}

func observeAlarmDispatchWakeupFailed() {
	initQueueMetrics()

	if alarmDispatchWakeupFailedTotal == nil {
		return
	}

	alarmDispatchWakeupFailedTotal.Inc()
}

func observeAlarmDispatchWakeupExpireFailed() {
	initQueueMetrics()

	if alarmDispatchWakeupExpireFailedTotal == nil {
		return
	}

	alarmDispatchWakeupExpireFailedTotal.Inc()
}
