package bootstrap

import (
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/webhook"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookMetricsObservesCountersAndHistograms(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := newWebhookMetrics(registry)

	metrics.ObserveRequest()
	metrics.ObserveUnauthorized()
	metrics.ObserveBadRequest()
	metrics.ObserveDuplicate()
	metrics.ObserveEnqueueFailure()
	metrics.ObserveAccepted()
	metrics.ObserveDecodeLatency(10 * time.Millisecond)
	metrics.ObserveHandlerDuration(40 * time.Millisecond)

	assertCounterOnce(t, metrics.requests)
	assertCounterOnce(t, metrics.unauthorized)
	assertCounterOnce(t, metrics.badRequests)
	assertCounterOnce(t, metrics.duplicates)
	assertCounterOnce(t, metrics.enqueueFailures)
	assertCounterOnce(t, metrics.accepted)
	assertHistogramCount(t, registry, "hololive_bot_webhook_decode_latency_seconds")
	assertHistogramCount(t, registry, "hololive_bot_webhook_handler_duration_seconds")
}

func assertCounterOnce(t *testing.T, collector prometheus.Collector) {
	t.Helper()

	if got := testutil.ToFloat64(collector); got != 1 {
		t.Fatalf("metric = %v, want 1", got)
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
	metrics := newWebhookMetrics(registry)
	metrics.BindSignatureDiagnostics(fixedWebhookSignatureDiagnostics{value: webhook.SignatureVersionDiagnostics{
		V3Validated:       3,
		UnknownRejected:   4,
		MalformedRejected: 5,
	}})

	want := map[string]float64{
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
		assert.InDelta(t, expected, family.Metric[0].GetCounter().GetValue(), 0)
		delete(want, family.GetName())
	}

	require.Empty(t, want)
}
