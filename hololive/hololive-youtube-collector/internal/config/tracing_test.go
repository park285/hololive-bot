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
}

func TestLoadTracingConfigSelectsOnlyInstanceToggle(t *testing.T) {
	tests := []struct {
		name        string
		instanceID  string
		selectedEnv string
	}{
		{name: "slot a", instanceID: "youtube-collector-a", selectedEnv: envload.TracingYouTubeCollectorAEnabledEnv},
		{name: "slot b", instanceID: "youtube-collector-b", selectedEnv: envload.TracingYouTubeCollectorBEnabledEnv},
		{name: "slot c", instanceID: "youtube-collector-c", selectedEnv: envload.TracingYouTubeCollectorCEnabledEnv},
		{name: "slot d", instanceID: "youtube-collector-d", selectedEnv: envload.TracingYouTubeCollectorDEnabledEnv},
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

// fleet slot 네 개 밖의 ID는 토글 값과 관계없이 기동 오류다. 빈 ID, 짧은 slot 이름, 대소문자 변형도 같다.
func TestLoadTracingConfigRejectsInstanceOutsideFleet(t *testing.T) {
	for _, instanceID := range []string{"", "c", "Youtube-Collector-C", "youtube-collector-e", "youtube-collector-legacy"} {
		t.Run(instanceID, func(t *testing.T) {
			settingstest.ClearTracingEnv(t)

			for _, key := range collectorTracingToggleEnvs {
				t.Setenv(key, "false")
			}

			_, err := loadTracingConfig(instanceID)
			if err == nil || !strings.Contains(err.Error(), "YOUTUBE_COLLECTOR_INSTANCE_ID must be one of youtube-collector-a") {
				t.Fatalf("loadTracingConfig(%q) error = %v, want instance ID validation error", instanceID, err)
			}
		})
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
