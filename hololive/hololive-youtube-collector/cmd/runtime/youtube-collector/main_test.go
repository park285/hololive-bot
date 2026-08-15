package main

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestYouTubeCollectorLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "youtube-collector-central.log")
	if got := youtubeCollectorLogFileName(); got != "youtube-collector-central.log" {
		t.Fatalf("youtubeCollectorLogFileName() = %q, want youtube-collector-central.log", got)
	}
}

func TestYouTubeCollectorLogFileNameRejectsPathSeparators(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "logs/youtube-collector.log")
	if got := youtubeCollectorLogFileName(); got != "youtube-collector.log" {
		t.Fatalf("youtubeCollectorLogFileName() = %q, want youtube-collector.log", got)
	}
}

func TestYouTubeCollectorTelemetryServiceNameIsStable(t *testing.T) {
	cfg := &settings.Config{
		Environment: "production",
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.1,
		},
	}
	got := youtubeCollectorTelemetryConfig(cfg, "3.4.5")
	if got.ServiceName != "youtube-collector" {
		t.Fatalf("ServiceName = %q, want youtube-collector", got.ServiceName)
	}
	if got.ServiceVersion != "3.4.5" || got.Environment != "production" {
		t.Fatalf("telemetry identity = %#v, want version and environment preserved", got)
	}
}
