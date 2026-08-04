package main

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestHololiveAPITelemetryConfigUsesFixedIdentity(t *testing.T) {
	appConfig := &settings.HololiveAPIConfig{
		Bot: &settings.Config{Environment: "production"},
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.25,
		},
	}

	got := hololiveAPITelemetryConfig(appConfig, "1.2.3")

	if got.ServiceName != "hololive-api" {
		t.Fatalf("ServiceName = %q, want hololive-api", got.ServiceName)
	}
	if got.ServiceVersion != "1.2.3" {
		t.Fatalf("ServiceVersion = %q, want 1.2.3", got.ServiceVersion)
	}
	if got.Environment != "production" {
		t.Fatalf("Environment = %q, want production", got.Environment)
	}
	if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.25 {
		t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
	}
}
