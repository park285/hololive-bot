package apiplane

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func validLLMSchedulerRuntimeConfig() *LLMSchedulerConfig {
	return &LLMSchedulerConfig{
		Server: settings.ServerConfig{
			Port:           30003,
			APIKey:         "x",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30003",
			H3CertFile:     settingstest.HololiveH3CertPath,
			H3KeyFile:      settingstest.HololiveH3KeyPath,
		},
		Postgres:    settings.PostgresConfig{SSLMode: load.PostgresSSLModeVerifyFull},
		Environment: load.EnvironmentProduction,
	}
}

func TestValidateLLMSchedulerRuntimeRejectsSchedulerWorkerRole(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationSchedulerRoleEnv, load.NotificationSchedulerRoleWorker)

	err := validLLMSchedulerRuntimeConfig().validateRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not run the alarm scheduler role") {
		t.Fatalf("LLMSchedulerConfig.validateRuntime() error = %v, want scheduler role rejection", err)
	}
}

func TestLoadLLMSchedulerRuntimeAllowsMissingIrisInputs(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	cfg, err := LoadLLMSchedulerRuntime()
	if err != nil {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v", err)
	}

	if cfg.Server.Port != 30003 {
		t.Fatalf("Server.Port = %d, want 30003", cfg.Server.Port)
	}

	if !cfg.Server.TransportEnabled("h3") {
		t.Fatal("Server.TransportEnabled(h3) = false, want true")
	}
}

// llm plane은 DELIVERY_OUTBOX_V3_HANDOFF_MODE를 읽던 delivery module을 소유했다. 퇴역한 키는 빈 값이어도 기동을 거절한다.
func TestLoadLLMSchedulerRuntimeRejectsRetiredOutboxV3HandoffMode(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	t.Setenv("DELIVERY_OUTBOX_V3_HANDOFF_MODE", "")

	_, err := LoadLLMSchedulerRuntime()
	if err == nil || !strings.Contains(err.Error(), "DELIVERY_OUTBOX_V3_HANDOFF_MODE is retired") {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v, want retired DELIVERY_OUTBOX_V3_HANDOFF_MODE rejection", err)
	}
}

// 아래 세 테스트는 비-runtime 로더(LoadLLMScheduler, stack-audit 2026-09-26 T11에서 삭제)로 검증하던 동작을 실제 runtime
// 로더에서 확인한다. 이 로더는 Iris 입력을 받지 않으므로 Iris env를 비운다.
func TestLoadLLMSchedulerRuntimeProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := LoadLLMSchedulerRuntime()
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v, want production sslmode rejection", err)
	}
}

func TestLoadLLMSchedulerRuntimeProductionRequiresAPISecretKey(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("API_SECRET_KEY", "")

	_, err := LoadLLMSchedulerRuntime()
	if err == nil || !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v, want production API key requirement", err)
	}
}

func TestLoadLLMSchedulerRuntimeEnvApplied(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("LLM_SCHEDULER_PORT", "39003")
	t.Setenv("BOT_PREFIX", "#")

	config, err := LoadLLMSchedulerRuntime()
	if err != nil {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v", err)
	}

	if config.Server.Port != 39003 {
		t.Fatalf("Server.Port = %d, want %d", config.Server.Port, 39003)
	}

	if config.Bot.Prefix != "#" {
		t.Fatalf("Bot.Prefix = %q, want %q", config.Bot.Prefix, "#")
	}
}

func TestLoadRuntimeSelectsHololiveAPITracingToggle(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	settingstest.ClearTracingEnv(t)
	settingstest.SetRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
	t.Setenv(load.HololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
	t.Setenv(load.TracingHololiveAPIEnabledEnv, "true")
	t.Setenv(load.TracingAlarmWorkerEnabledEnv, "not-a-bool")

	for _, key := range load.TracingEnabledEnvKeys()[2:] {
		t.Setenv(key, "not-a-bool")
	}

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if !config.Tracing.Enabled || config.Tracing != config.Bot.Tracing || config.Tracing != config.Admin.Tracing {
		t.Fatalf("RuntimeConfig tracing = %#v, bot = %#v, admin = %#v", config.Tracing, config.Bot.Tracing, config.Admin.Tracing)
	}
}
