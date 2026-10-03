package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setRuntimeEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	settingstest.SetRequiredLoadEnv(t)
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")
	t.Setenv("APP_ENV", "development")
}

func TestLoadRuntimeRejectsInvalidDispatchRetentionEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", "0")

	_, err := LoadRuntime()
	if err == nil {
		t.Fatal("LoadRuntime() error = nil, want alarm dispatch retention rejection")
	}

	if !strings.Contains(err.Error(), "load alarm dispatch retention config: ") || !strings.Contains(err.Error(), "ALARM_DISPATCH_RETENTION_INTERVAL_MS") {
		t.Fatalf("LoadRuntime() error = %v, want wrapped alarm dispatch retention rejection", err)
	}
}

func TestLoadRuntimeIgnoresInvalidYouTubeCollectorEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "INVALID")

	if _, err := LoadRuntime(); err != nil {
		t.Fatalf("LoadRuntime() error = %v, want success when collector env is invalid", err)
	}
}

func TestLoadRuntimeSelectsAlarmWorkerTracingToggle(t *testing.T) {
	setRuntimeEnv(t)
	settingstest.ClearTracingEnv(t)
	t.Setenv(envload.HololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
	t.Setenv(envload.TracingHololiveAPIEnabledEnv, "not-a-bool")
	t.Setenv(envload.TracingAlarmWorkerEnabledEnv, "true")

	for _, key := range envload.TracingEnabledEnvKeys()[2:] {
		t.Setenv(key, "not-a-bool")
	}

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if !config.Tracing.Enabled {
		t.Fatal("TracingConfig.Enabled = false, want true")
	}
}
