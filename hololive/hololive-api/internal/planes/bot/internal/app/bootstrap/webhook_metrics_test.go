package bootstrap

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestWebhookMetricsObservesCountersAndHistograms(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewWebhookMetrics(registry)

	metrics.ObserveRequest()
	metrics.ObserveUnauthorized()
	metrics.ObserveBadRequest()
	metrics.ObserveDuplicate()
	metrics.ObserveEnqueueFailure()
	metrics.ObserveAccepted()
	metrics.ObserveDecodeLatency(10 * time.Millisecond)
	metrics.ObserveDedupLatency(20 * time.Millisecond)
	metrics.ObserveEnqueueWait(30 * time.Millisecond)
	metrics.ObserveQueueDepth(7)
	metrics.ObserveHandlerDuration(40 * time.Millisecond)

	assertMetricValue(t, metrics.requests, 1)
	assertMetricValue(t, metrics.unauthorized, 1)
	assertMetricValue(t, metrics.badRequests, 1)
	assertMetricValue(t, metrics.duplicates, 1)
	assertMetricValue(t, metrics.enqueueFailures, 1)
	assertMetricValue(t, metrics.accepted, 1)
	assertMetricValue(t, metrics.queueDepth, 7)
	assertHistogramCount(t, registry, "hololive_bot_webhook_decode_latency_seconds")
	assertHistogramCount(t, registry, "hololive_bot_webhook_dedup_latency_seconds")
	assertHistogramCount(t, registry, "hololive_bot_webhook_enqueue_wait_seconds")
	assertHistogramCount(t, registry, "hololive_bot_webhook_handler_duration_seconds")
}

func assertMetricValue(t *testing.T, collector prometheus.Collector, want float64) {
	t.Helper()
	if got := testutil.ToFloat64(collector); got != want {
		t.Fatalf("metric = %v, want %v", got, want)
	}
}

func assertHistogramCount(t *testing.T, registry prometheus.Gatherer, name string) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			if got := family.Metric[0].GetHistogram().GetSampleCount(); got != 1 {
				t.Fatalf("metric %s sample count = %v, want 1", name, got)
			}
			return
		}
	}
	t.Fatalf("metric %s was not gathered", name)
}
