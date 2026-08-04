package main

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestYouTubeProducerLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_LOG_FILE_NAME", "youtube-producer-b.log")

	if got := youtubeProducerLogFileName(); got != "youtube-producer-b.log" {
		t.Fatalf("youtubeProducerLogFileName() = %q, want %q", got, "youtube-producer-b.log")
	}
}

func TestYouTubeProducerLogFileNameDefaultsToLegacyName(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_LOG_FILE_NAME", "")

	if got := youtubeProducerLogFileName(); got != "youtube-producer.log" {
		t.Fatalf("youtubeProducerLogFileName() = %q, want %q", got, "youtube-producer.log")
	}
}

func TestYouTubeProducerTelemetryConfigUsesFixedInstanceIdentity(t *testing.T) {
	tests := []struct {
		instanceID string
		want       string
	}{
		{instanceID: "a", want: "youtube-producer-a"},
		{instanceID: "youtube-producer-b", want: "youtube-producer-b"},
		{instanceID: "c", want: "youtube-producer-c"},
		{instanceID: "youtube-producer-d", want: "youtube-producer-d"},
	}

	for _, tt := range tests {
		t.Run(tt.instanceID, func(t *testing.T) {
			appConfig := &settings.Config{
				Environment: "production",
				Tracing: settings.TracingConfig{
					Enabled:    true,
					Endpoint:   "otel-collector:4317",
					Insecure:   true,
					SampleRate: 0.1,
				},
			}
			appConfig.Scraper.ActiveActive.InstanceID = tt.instanceID

			got, err := youtubeProducerTelemetryConfig(appConfig, "3.4.5")
			if err != nil {
				t.Fatalf("youtubeProducerTelemetryConfig() error = %v", err)
			}
			if got.ServiceName != tt.want {
				t.Fatalf("ServiceName = %q, want %q", got.ServiceName, tt.want)
			}
			if got.ServiceVersion != "3.4.5" || got.Environment != "production" {
				t.Fatalf("telemetry identity = %#v, want version and environment preserved", got)
			}
			if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.1 {
				t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
			}
		})
	}
}

func TestYouTubeProducerTelemetryConfigRejectsUnknownEnabledInstance(t *testing.T) {
	appConfig := &settings.Config{Tracing: settings.TracingConfig{Enabled: true}}
	appConfig.Scraper.ActiveActive.InstanceID = "custom-producer"

	if _, err := youtubeProducerTelemetryConfig(appConfig, "dev"); err == nil {
		t.Fatal("youtubeProducerTelemetryConfig() error = nil, want unsupported instance error")
	}
}

func TestYouTubeProducerTelemetryConfigDisabledIsNoOpWithoutInstance(t *testing.T) {
	appConfig := &settings.Config{Tracing: settings.TracingConfig{Enabled: false}}

	got, err := youtubeProducerTelemetryConfig(appConfig, "dev")
	if err != nil {
		t.Fatalf("youtubeProducerTelemetryConfig() error = %v", err)
	}
	if got.Enabled {
		t.Fatal("disabled telemetry config was enabled")
	}
	if got.ServiceName != "youtube-producer" {
		t.Fatalf("ServiceName = %q, want disabled fallback identity", got.ServiceName)
	}
}
