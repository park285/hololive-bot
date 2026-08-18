package bootstrap

import (
	"testing"
	"time"

	"github.com/park285/iris-client-go/webhook"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type fixedWebhookSignatureDiagnostics struct {
	value webhook.SignatureVersionDiagnostics
}

func (f fixedWebhookSignatureDiagnostics) SignatureVersionDiagnostics() webhook.SignatureVersionDiagnostics {
	return f.value
}

func TestWebhookMetricsExposeSignatureVersionDiagnostics(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	metrics := NewWebhookMetrics(registry)
	metrics.BindSignatureDiagnostics(fixedWebhookSignatureDiagnostics{value: webhook.SignatureVersionDiagnostics{
		V2Validated:       2,
		V3Validated:       3,
		UnknownRejected:   4,
		MalformedRejected: 5,
	}})

	want := map[string]float64{
		"hololive_bot_webhook_signature_v2_validated_total":       2,
		"hololive_bot_webhook_signature_v3_validated_total":       3,
		"hololive_bot_webhook_signature_unknown_rejected_total":   4,
		"hololive_bot_webhook_signature_malformed_rejected_total": 5,
	}
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		expected, ok := want[family.GetName()]
		if !ok {
			continue
		}
		require.NotEmpty(t, family.Metric)
		assert.Equal(t, expected, family.Metric[0].GetCounter().GetValue())
		delete(want, family.GetName())
	}
	require.Empty(t, want)
}
