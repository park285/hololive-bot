package queue

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	queueMetricsInitOnce sync.Once

	alarmQueueDrainDuration prometheus.Histogram
	alarmQueueDrainBatch    prometheus.Histogram
	alarmQueueDrainTotal    *prometheus.CounterVec
	alarmQueueEnvelopeTotal *prometheus.CounterVec
	alarmQueueClaimReleased prometheus.Counter
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
	})
}
