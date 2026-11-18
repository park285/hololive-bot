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
