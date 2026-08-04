package main

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestAlarmWorkerTelemetryConfigUsesFixedIdentity(t *testing.T) {
	appConfig := &settings.Config{
		Environment: "staging",
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.1,
		},
	}

	got := alarmWorkerTelemetryConfig(appConfig, "2.3.4")

	if got.ServiceName != "hololive-alarm-worker" {
		t.Fatalf("ServiceName = %q, want hololive-alarm-worker", got.ServiceName)
	}
	if got.ServiceVersion != "2.3.4" {
		t.Fatalf("ServiceVersion = %q, want 2.3.4", got.ServiceVersion)
	}
	if got.Environment != "staging" {
		t.Fatalf("Environment = %q, want staging", got.Environment)
	}
	if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.1 {
		t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
	}
}
