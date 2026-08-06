package main

import (
	"os"
	"strings"
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

func TestYouTubeProducerTelemetryConfigDerivesInstanceIdentity(t *testing.T) {
	tests := []struct {
		instanceID string
		want       string
	}{
		{instanceID: "a", want: "youtube-producer-a"},
		{instanceID: "youtube-producer-b", want: "youtube-producer-b"},
		{instanceID: "c", want: "youtube-producer-c"},
		{instanceID: "youtube-producer-d", want: "youtube-producer-d"},
		{instanceID: "youtube-producer-e", want: "youtube-producer-e"},
		{instanceID: "custom-producer", want: "youtube-producer-custom-producer"},
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

			got := youtubeProducerTelemetryConfig(appConfig, "3.4.5")
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

func TestYouTubeProducerTelemetryServiceNameIsSameRegardlessOfTracingEnabled(t *testing.T) {
	enabled := &settings.Config{Tracing: settings.TracingConfig{Enabled: true}}
	enabled.Scraper.ActiveActive.InstanceID = "d"
	disabled := &settings.Config{Tracing: settings.TracingConfig{Enabled: false}}
	disabled.Scraper.ActiveActive.InstanceID = "d"

	if a, b := youtubeProducerTelemetryConfig(enabled, "dev").ServiceName, youtubeProducerTelemetryConfig(disabled, "dev").ServiceName; a != b {
		t.Fatalf("ServiceName diverges on Tracing.Enabled: enabled=%q disabled=%q", a, b)
	}
}

func TestYouTubeProducerTelemetryServiceNameWithoutInstanceFallsBackToHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		t.Skip("hostname unavailable")
	}
	want := "youtube-producer-" + strings.ToLower(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hostname)), "youtube-producer-"))

	if got := youtubeProducerTelemetryServiceName(""); got != want {
		t.Fatalf("ServiceName = %q, want hostname-derived %q", got, want)
	}
}
