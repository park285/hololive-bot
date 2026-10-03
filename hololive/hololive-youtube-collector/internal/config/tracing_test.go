package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

var collectorTracingToggleEnvs = []string{
	envload.TracingYouTubeCollectorAEnabledEnv,
	envload.TracingYouTubeCollectorBEnabledEnv,
	envload.TracingYouTubeCollectorCEnabledEnv,
	envload.TracingYouTubeCollectorDEnabledEnv,
	envload.TracingYouTubeCollectorEnabledEnv,
}

func TestLoadTracingConfigSelectsOnlyInstanceToggle(t *testing.T) {
	tests := []struct {
		name        string
		instanceID  string
		selectedEnv string
	}{
		{name: "slot a", instanceID: "a", selectedEnv: envload.TracingYouTubeCollectorAEnabledEnv},
		{name: "slot b", instanceID: "b", selectedEnv: envload.TracingYouTubeCollectorBEnabledEnv},
		{name: "slot c", instanceID: "c", selectedEnv: envload.TracingYouTubeCollectorCEnabledEnv},
		{name: "slot d", instanceID: "d", selectedEnv: envload.TracingYouTubeCollectorDEnabledEnv},
		{name: "prefixed slot", instanceID: " Youtube-Collector-C ", selectedEnv: envload.TracingYouTubeCollectorCEnabledEnv},
		{name: "empty instance default", selectedEnv: envload.TracingYouTubeCollectorEnabledEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settingstest.ClearTracingEnv(t)

			for _, key := range envload.TracingEnabledEnvKeys() {
				t.Setenv(key, "not-a-bool")
			}

			t.Setenv(tt.selectedEnv, "true")
			t.Setenv("OTEL_ENABLED", "true")
			t.Setenv(envload.HololiveOTLPGRPCEndpointEnv, " otel-collector:4317 ")

			config, err := loadTracingConfig(tt.instanceID)
			if err != nil {
				t.Fatalf("loadTracingConfig() error = %v", err)
			}

			if !config.Enabled {
				t.Fatal("TracingConfig.Enabled = false, want true")
			}

			if config.Endpoint != "otel-collector:4317" {
				t.Fatalf("TracingConfig.Endpoint = %q, want otel-collector:4317", config.Endpoint)
			}
		})
	}
}

func TestLoadTracingConfigRejectsUnknownInstanceWithEnabledToggle(t *testing.T) {
	settingstest.ClearTracingEnv(t)

	for _, key := range envload.TracingEnabledEnvKeys() {
		t.Setenv(key, "true")
	}

	t.Setenv("OTEL_ENABLED", "true")

	_, err := loadTracingConfig("unknown")
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_COLLECTOR_INSTANCE_ID must be one of a, b, c, d, youtube-collector-a") {
		t.Fatalf("loadTracingConfig() error = %v, want instance ID validation error", err)
	}
}

// 표준 OTLP endpoint env 거부는 instance ID 판정보다 먼저 보인다.
func TestLoadTracingConfigRejectsStandardOTLPEndpointBeforeInstance(t *testing.T) {
	for _, standardEnv := range []string{envload.OTLPEndpointEnv, envload.OTLPTracesEndpointEnv} {
		t.Run(standardEnv, func(t *testing.T) {
			settingstest.ClearTracingEnv(t)

			for _, key := range collectorTracingToggleEnvs {
				t.Setenv(key, "true")
			}

			t.Setenv(standardEnv, "otel-collector:4317")

			_, err := loadTracingConfig("unknown")
			if err == nil || !strings.Contains(err.Error(), standardEnv+" is not accepted by Hololive runtimes") {
				t.Fatalf("loadTracingConfig() error = %v, want standard endpoint rejection", err)
			}

			if strings.Contains(err.Error(), "YOUTUBE_COLLECTOR_INSTANCE_ID") {
				t.Fatalf("loadTracingConfig() error = %v, instance ID error must not precede standard endpoint rejection", err)
			}
		})
	}
}

func TestLoadTracingConfigAllowsDisabledUnknownInstance(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		setFlags   bool
	}{
		{name: "empty instance and unset flags"},
		{name: "unknown instance and false flags", instanceID: "youtube-collector-legacy", setFlags: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settingstest.ClearTracingEnv(t)

			if tt.setFlags {
				for _, key := range collectorTracingToggleEnvs {
					t.Setenv(key, "false")
				}
			}

			config, err := loadTracingConfig(tt.instanceID)
			if err != nil {
				t.Fatalf("loadTracingConfig() error = %v, want nil", err)
			}

			if config.Enabled {
				t.Fatal("TracingConfig.Enabled = true, want false")
			}
		})
	}
}

// 알 수 없는 instance ID의 허용 판정이 읽는 collector 토글은 잘못된 bool이면 기동을 막는다.
func TestLoadTracingConfigRejectsInvalidToggleForUnknownInstance(t *testing.T) {
	settingstest.ClearTracingEnv(t)
	t.Setenv(envload.TracingYouTubeCollectorBEnabledEnv, "not-a-bool")

	_, err := loadTracingConfig("unknown")
	if err == nil || !strings.Contains(err.Error(), envload.TracingYouTubeCollectorBEnabledEnv) {
		t.Fatalf("loadTracingConfig() error = %v, want invalid toggle error", err)
	}
}
