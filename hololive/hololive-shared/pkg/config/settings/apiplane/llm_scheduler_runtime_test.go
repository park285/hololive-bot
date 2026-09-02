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

func TestLoadLLMSchedulerStillRequiresIrisTokens(t *testing.T) {
	settingstest.ClearIrisAndRoomEnv(t)
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	_, err := LoadLLMScheduler()
	if err == nil || !strings.Contains(err.Error(), settingstest.IrisWebhookTokenEnv) {
		t.Fatalf("LoadLLMScheduler() error = %v, want IRIS_WEBHOOK_TOKEN requirement", err)
	}
}

func TestLoadLLMSchedulerProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	settingstest.SetH3CertificateEnv(t)
	t.Setenv(settingstest.IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected production sslmode validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLLMSchedulerProductionRequiresAPISecretKey(t *testing.T) {
	settingstest.SetH3CertificateEnv(t)
	t.Setenv(settingstest.IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("API_SECRET_KEY", "")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected production API key validation error, got nil")
	}

	if !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLLMSchedulerEnvApplied(t *testing.T) {
	settingstest.SetH3CertificateEnv(t)
	t.Setenv(settingstest.IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("LLM_SCHEDULER_PORT", "39003")
	t.Setenv("BOT_PREFIX", "#")

	config, err := LoadLLMScheduler()
	if err != nil {
		t.Fatalf("LoadLLMScheduler() error = %v", err)
	}

	if config.Server.Port != 39003 {
		t.Fatalf("Server.Port = %d, want %d", config.Server.Port, 39003)
	}

	if config.Bot.Prefix != "#" {
		t.Fatalf("Bot.Prefix = %q, want %q", config.Bot.Prefix, "#")
	}
}

func TestLoadLLMSchedulerIrisSharedTokenNoLongerProvidesFallback(t *testing.T) {
	settingstest.SetH3CertificateEnv(t)
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv(settingstest.IrisWebhookTokenEnv, "")
	t.Setenv(settingstest.IrisBotTokenEnv, "")
	t.Setenv("API_SECRET_KEY", "test-api-key")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected missing webhook token error, got nil")
	}

	if !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
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
