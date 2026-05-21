package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should be disabled")
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("expected sample rate 1.0, got %f", cfg.SampleRate)
	}
	if !cfg.OTLPInsecure {
		t.Error("default should be insecure")
	}
}

func TestNewProvider_Disabled(t *testing.T) {
	cfg := Config{Enabled: false}
	provider, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("provider should not be nil")
	}
	if provider.IsEnabled() {
		t.Error("disabled provider should return false for IsEnabled")
	}
}

func TestProvider_Shutdown_Nil(t *testing.T) {
	provider := &Provider{}
	err := provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("shutdown on nil provider should not error: %v", err)
	}
}

func TestProvider_IsEnabled(t *testing.T) {
	t.Run("nil tracer provider", func(t *testing.T) {
		p := &Provider{tracerProvider: nil}
		if p.IsEnabled() {
			t.Error("should return false for nil tracerProvider")
		}
	})
}

func TestBuildResource_PopulatesServiceAttrs(t *testing.T) {
	cfg := Config{
		ServiceName:    "hololive-test",
		ServiceVersion: "1.2.3",
		Environment:    "test",
	}

	res := buildResource(cfg)
	if res.SchemaURL() != semconv.SchemaURL {
		t.Fatalf("expected schema URL %q, got %q", semconv.SchemaURL, res.SchemaURL())
	}

	attrs := attribute.NewSet(res.Attributes()...)
	got := make(map[attribute.Key]string)
	iter := attrs.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		got[kv.Key] = kv.Value.AsString()
	}

	assertAttributeValue(t, got, semconv.ServiceNameKey, cfg.ServiceName)
	assertAttributeValue(t, got, semconv.ServiceVersionKey, cfg.ServiceVersion)
	assertAttributeValue(t, got, semconv.DeploymentEnvironmentKey, cfg.Environment)
}

func TestBuildSampler_AlwaysSample(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 1.0})

	if !strings.Contains(sampler.Description(), "AlwaysOnSampler") {
		t.Fatalf("expected AlwaysOnSampler in description, got %q", sampler.Description())
	}

	for _, traceID := range []trace.TraceID{
		{0x01},
		{0x02},
		{0xff},
	} {
		result := sampler.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       traceID,
			Name:          "test",
		})
		if result.Decision != sdktrace.RecordAndSample {
			t.Fatalf("expected RecordAndSample for trace ID %v, got %v", traceID, result.Decision)
		}
	}
}

func TestBuildSampler_NeverSample(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 0})

	if !strings.Contains(sampler.Description(), "AlwaysOffSampler") {
		t.Fatalf("expected AlwaysOffSampler in description, got %q", sampler.Description())
	}

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{0x01},
		Name:          "test",
	})
	if result.Decision != sdktrace.Drop {
		t.Fatalf("expected Drop, got %v", result.Decision)
	}
}

func TestBuildSampler_TraceIDRatioBased(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 0.5})

	if !strings.Contains(sampler.Description(), "TraceIDRatioBased{0.5}") {
		t.Fatalf("expected ratio sampler in description, got %q", sampler.Description())
	}
}

func TestBuildOTLPExporterOptions_Endpoint(t *testing.T) {
	opts := buildOTLPExporterOptions(Config{
		OTLPEndpoint: "otel-collector:4317",
		OTLPInsecure: false,
	})

	if len(opts) != 1 {
		t.Fatalf("expected endpoint option only, got %d options", len(opts))
	}
}

func TestBuildOTLPExporterOptions_InsecureTrue(t *testing.T) {
	opts := buildOTLPExporterOptions(Config{
		OTLPEndpoint: "otel-collector:4317",
		OTLPInsecure: true,
	})

	if len(opts) != 2 {
		t.Fatalf("expected endpoint and insecure dial options, got %d options", len(opts))
	}
}

func TestBuildOTLPExporterOptions_InsecureFalse(t *testing.T) {
	opts := buildOTLPExporterOptions(Config{
		OTLPEndpoint: "otel-collector:4317",
		OTLPInsecure: false,
	})

	if len(opts) != 1 {
		t.Fatalf("expected insecure=false to keep only endpoint option, got %d options", len(opts))
	}
}

func TestMapCarrier_GetSetKeys(t *testing.T) {
	carrier := MapCarrier{}

	carrier.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	carrier.Set("baggage", "tenant=test")

	if got := carrier.Get("traceparent"); got != "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01" {
		t.Fatalf("unexpected traceparent value: %q", got)
	}

	keys := carrier.Keys()
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	if !keySet["traceparent"] || !keySet["baggage"] {
		t.Fatalf("expected traceparent and baggage keys, got %v", keys)
	}
}

func assertAttributeValue(t *testing.T, attrs map[attribute.Key]string, key attribute.Key, want string) {
	t.Helper()

	if got, ok := attrs[key]; !ok || got != want {
		t.Fatalf("expected attribute %s=%q, got %q (present=%v)", key, want, got, ok)
	}
}
