package app

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func setRequiredEnvForLoadConfig(t *testing.T) {
	t.Helper()
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
}

func TestLoadConfig_UsesIRISSharedTokenFallback(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("IRIS_BOT_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Iris.BotToken != "shared-token" {
		t.Fatalf("Iris.BotToken = %q, want %q", cfg.Iris.BotToken, "shared-token")
	}
}

func TestLoadConfig_AppliesDefaultWhenInvalidNumericValue(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "-1")
	t.Setenv("DISPATCHER_RECONNECT_BACKOFF_MS", "0")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Dispatch.MaxBatch != defaultMaxBatch {
		t.Fatalf("Dispatch.MaxBatch = %d, want %d", cfg.Dispatch.MaxBatch, defaultMaxBatch)
	}
	if cfg.Dispatch.ReconnectBackoff != time.Second {
		t.Fatalf("Dispatch.ReconnectBackoff = %v, want %v", cfg.Dispatch.ReconnectBackoff, time.Second)
	}
}

func TestLoadConfig_TelemetryMetricsIntervalFallback(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("OTEL_METRICS_EXPORT_INTERVAL_SECONDS", "-1")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Telemetry.MetricsExportInterval != 30*time.Second {
		t.Fatalf("Telemetry.MetricsExportInterval = %v, want %v", cfg.Telemetry.MetricsExportInterval, 30*time.Second)
	}
}

func TestLoadConfig_LegacyValkeyEnvFallback(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("CACHE_HOST", "")
	t.Setenv("CACHE_PORT", "")
	t.Setenv("VALKEY_HOST", "legacy-valkey")
	t.Setenv("VALKEY_PORT", "6381")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Valkey.Host != "legacy-valkey" {
		t.Fatalf("Valkey.Host = %q, want %q", cfg.Valkey.Host, "legacy-valkey")
	}
	if cfg.Valkey.Port != 6381 {
		t.Fatalf("Valkey.Port = %d, want %d", cfg.Valkey.Port, 6381)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidate_RequiresBotToken(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL: "http://localhost:3000",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}
