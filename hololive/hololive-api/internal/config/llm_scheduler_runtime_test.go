package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
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
		Postgres:    settings.PostgresConfig{SSLMode: runtimepolicy.PostgresSSLModeVerifyFull},
		Environment: runtimepolicy.EnvironmentProduction,
		LLMProvider: LLMProviderCliproxy,
	}
}

func TestValidateLLMSchedulerRuntimeRejectsSchedulerWorkerRole(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, runtimepolicy.NotificationSchedulerRoleWorker)

	err := validLLMSchedulerRuntimeConfig().validateRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not run the alarm scheduler role") {
		t.Fatalf("LLMSchedulerConfig.validateRuntime() error = %v, want scheduler role rejection", err)
	}
}

func TestValidateLLMSchedulerRuntimeProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	config := validLLMSchedulerRuntimeConfig()

	config.Postgres.SSLMode = "require"

	err := config.validateRuntime()
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("LLMSchedulerConfig.validateRuntime() error = %v, want production sslmode rejection", err)
	}
}

func TestValidateLLMSchedulerRuntimeProductionRequiresAPISecretKey(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	config := validLLMSchedulerRuntimeConfig()

	config.Server.APIKey = ""

	err := config.validateRuntime()
	if err == nil || !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("LLMSchedulerConfig.validateRuntime() error = %v, want production API key requirement", err)
	}
}

// llm plane listener는 configurePlanes의 LLM_SCHEDULER_PORT 한 번 읽기에서, 나머지 구획은 llm plane 로더에서 온다.
func TestLoadRuntimeAppliesLLMPlaneEnv(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	configtest.SetRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
	t.Setenv("LLM_SCHEDULER_PORT", "39003")
	t.Setenv("BOT_PREFIX", "#")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if config.LLM.Server.Port != 39003 || config.LLM.Server.H3Addr != ":39003" {
		t.Fatalf("LLM server port = %d addr = %q, want 39003 and :39003", config.LLM.Server.Port, config.LLM.Server.H3Addr)
	}

	if config.LLM.Bot.Prefix != "#" {
		t.Fatalf("LLM Bot.Prefix = %q, want %q", config.LLM.Bot.Prefix, "#")
	}
}

func TestLoadRuntimeCapturesInternalClientAndMemberNewsInputs(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	configtest.SetRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", " http://127.0.0.1:30007 ")
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", " /internal-ca.crt ")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", " internal.example ")
	t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", " /member-news-accounts.json ")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if config.Bot.InternalH3.CACertFile != "/internal-ca.crt" || config.Bot.InternalH3.ServerName != "internal.example" {
		t.Fatalf("bot internal H3 settings = %+v, want trimmed startup values", config.Bot.InternalH3)
	}

	if config.Admin.InternalH3.CACertFile != config.Bot.InternalH3.CACertFile || config.Admin.InternalH3.ServerName != config.Bot.InternalH3.ServerName {
		t.Fatal("admin internal H3 settings differ from loaded bot startup values")
	}

	if config.LLM.MemberNewsXAllowlistPath != "/member-news-accounts.json" {
		t.Fatalf("LLM member news allowlist path = %q, want trimmed startup path", config.LLM.MemberNewsXAllowlistPath)
	}

	if config.Bot.AlarmServiceURL != "http://127.0.0.1:30007" || config.Admin.AlarmServiceURL != config.Bot.AlarmServiceURL {
		t.Fatalf("alarm service URL bot=%q admin=%q, want the same trimmed startup value", config.Bot.AlarmServiceURL, config.Admin.AlarmServiceURL)
	}
}
