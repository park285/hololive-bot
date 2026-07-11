package bootstrap

import (
	"sync"
	"time"

	"github.com/park285/iris-client-go/webhook"
	"github.com/prometheus/client_golang/prometheus"
)

const webhookMetricPrefix = "hololive_bot_webhook_"

type webhookMetrics struct {
	requests        prometheus.Counter
	unauthorized    prometheus.Counter
	badRequests     prometheus.Counter
	duplicates      prometheus.Counter
	enqueueFailures prometheus.Counter
	accepted        prometheus.Counter
	decodeLatency   prometheus.Histogram
	dedupLatency    prometheus.Histogram
	enqueueWait     prometheus.Histogram
	queueDepth      prometheus.Gauge
	handlerDuration prometheus.Histogram
}

var (
	defaultWebhookMetricsOnce  sync.Once
	defaultWebhookMetricsValue *webhookMetrics
)

var _ webhook.Metrics = (*webhookMetrics)(nil)

func NewWebhookMetrics(registerer prometheus.Registerer) *webhookMetrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &webhookMetrics{
		requests:        prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "requests_total", Help: "Total inbound bot webhook requests."}),
		unauthorized:    prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "unauthorized_total", Help: "Total unauthorized bot webhook requests."}),
		badRequests:     prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "bad_request_total", Help: "Total malformed bot webhook requests."}),
		duplicates:      prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "duplicates_total", Help: "Total duplicate bot webhook requests."}),
		enqueueFailures: prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "enqueue_failures_total", Help: "Total bot webhook enqueue failures."}),
		accepted:        prometheus.NewCounter(prometheus.CounterOpts{Name: webhookMetricPrefix + "accepted_total", Help: "Total accepted bot webhook requests."}),
		decodeLatency:   prometheus.NewHistogram(prometheus.HistogramOpts{Name: webhookMetricPrefix + "decode_latency_seconds", Help: "Bot webhook body decode latency in seconds.", Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12)}),
		dedupLatency:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: webhookMetricPrefix + "dedup_latency_seconds", Help: "Bot webhook deduplication latency in seconds.", Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12)}),
		enqueueWait:     prometheus.NewHistogram(prometheus.HistogramOpts{Name: webhookMetricPrefix + "enqueue_wait_seconds", Help: "Bot webhook queue enqueue wait in seconds.", Buckets: prometheus.DefBuckets}),
		queueDepth:      prometheus.NewGauge(prometheus.GaugeOpts{Name: webhookMetricPrefix + "queue_depth", Help: "Current bot webhook queue depth."}),
		handlerDuration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: webhookMetricPrefix + "handler_duration_seconds", Help: "Bot webhook message handler duration in seconds.", Buckets: prometheus.DefBuckets}),
	}
	registerer.MustRegister(
		metrics.requests, metrics.unauthorized, metrics.badRequests, metrics.duplicates,
		metrics.enqueueFailures, metrics.accepted, metrics.decodeLatency, metrics.dedupLatency,
		metrics.enqueueWait, metrics.queueDepth, metrics.handlerDuration,
	)
	return metrics
}

func defaultWebhookMetrics() webhook.Metrics {
	defaultWebhookMetricsOnce.Do(func() {
		defaultWebhookMetricsValue = NewWebhookMetrics(prometheus.DefaultRegisterer)
	})
	return defaultWebhookMetricsValue
}

func (m *webhookMetrics) ObserveRequest()                      { m.requests.Inc() }
func (m *webhookMetrics) ObserveUnauthorized()                 { m.unauthorized.Inc() }
func (m *webhookMetrics) ObserveBadRequest()                   { m.badRequests.Inc() }
func (m *webhookMetrics) ObserveDuplicate()                    { m.duplicates.Inc() }
func (m *webhookMetrics) ObserveEnqueueFailure()               { m.enqueueFailures.Inc() }
func (m *webhookMetrics) ObserveAccepted()                     { m.accepted.Inc() }
func (m *webhookMetrics) ObserveDecodeLatency(d time.Duration) { m.decodeLatency.Observe(d.Seconds()) }
func (m *webhookMetrics) ObserveDedupLatency(d time.Duration)  { m.dedupLatency.Observe(d.Seconds()) }
func (m *webhookMetrics) ObserveEnqueueWait(d time.Duration)   { m.enqueueWait.Observe(d.Seconds()) }
func (m *webhookMetrics) ObserveQueueDepth(depth int)          { m.queueDepth.Set(float64(depth)) }
func (m *webhookMetrics) ObserveHandlerDuration(d time.Duration) {
	m.handlerDuration.Observe(d.Seconds())
}
