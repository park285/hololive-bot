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

type queueMetricSet struct {
	publishBatchDuration     prometheus.Histogram
	publishRequestedTotal    prometheus.Counter
	publishProcessedTotal    prometheus.Counter
	publishInsertedTotal     prometheus.Counter
	publishDuplicateTotal    prometheus.Counter
	publishHashConflictTotal prometheus.Counter
	wakeupSentTotal          prometheus.Counter
	wakeupSuppressedTotal    prometheus.Counter
	wakeupFailedTotal        prometheus.Counter
	wakeupExpireFailedTotal  prometheus.Counter
}

// queueMetrics는 NewPublisher가 먼저 호출해, label 없는 publish·wakeup 지표를 기동부터 0으로 노출한다.
var queueMetrics = sync.OnceValue(func() *queueMetricSet {
	return &queueMetricSet{
		publishBatchDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "alarm_dispatch_publish_batch_duration_seconds",
				Help:    "Alarm dispatch publisher batch duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
		publishRequestedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_requested_deliveries_total",
			Help: "Requested alarm dispatch deliveries published.",
		}),
		publishProcessedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_processed_deliveries_total",
			Help: "Alarm dispatch deliveries successfully processed by the active publish mode.",
		}),
		publishInsertedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_inserted_deliveries_total",
			Help: "Inserted alarm dispatch deliveries.",
		}),
		publishDuplicateTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_duplicate_deliveries_total",
			Help: "Duplicate alarm dispatch deliveries skipped.",
		}),
		publishHashConflictTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_publish_hash_conflict_total",
			Help: "Alarm dispatch event hash conflicts observed while publishing.",
		}),
		wakeupSentTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_wakeup_sent_total",
			Help: "Alarm dispatch wakeup tokens sent.",
		}),
		wakeupSuppressedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_wakeup_suppressed_total",
			Help: "Alarm dispatch wakeup tokens suppressed by guard.",
		}),
		wakeupFailedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_wakeup_failed_total",
			Help: "Alarm dispatch wakeup token send failures.",
		}),
		wakeupExpireFailedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_wakeup_expire_failed_total",
			Help: "Alarm dispatch wakeup list TTL failures.",
		}),
	}
})

func observeAlarmDispatchPublishBatch(duration time.Duration, result *dispatchoutbox.PublishBatchResult) {
	metrics := queueMetrics()
	metrics.publishBatchDuration.Observe(duration.Seconds())
	metrics.publishRequestedTotal.Add(float64(result.RequestedDeliveries))
	metrics.publishProcessedTotal.Add(float64(result.ProcessedDeliveries))
	metrics.publishInsertedTotal.Add(float64(result.InsertedDeliveries))
	metrics.publishDuplicateTotal.Add(float64(result.DuplicateDeliveries))
	metrics.publishHashConflictTotal.Add(float64(result.HashConflictEvents))
}

func observeAlarmDispatchWakeupSent() {
	queueMetrics().wakeupSentTotal.Inc()
}

func observeAlarmDispatchWakeupSuppressed() {
	queueMetrics().wakeupSuppressedTotal.Inc()
}

func observeAlarmDispatchWakeupFailed() {
	queueMetrics().wakeupFailedTotal.Inc()
}

func observeAlarmDispatchWakeupExpireFailed() {
	queueMetrics().wakeupExpireFailedTotal.Inc()
}
