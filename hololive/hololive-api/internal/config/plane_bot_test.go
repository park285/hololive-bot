package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setBotPlaneEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	settingstest.ClearTracingEnv(t)
	settingstest.SetRequiredLoadEnv(t)
}

func TestLoadBotPlaneRuntimeRejectsNonPositiveHolodexTimeout(t *testing.T) {
	setBotPlaneEnv(t)
	t.Setenv("HOLODEX_TIMEOUT_SECONDS", "0")

	_, err := LoadBotPlaneRuntime()
	if err == nil || !strings.Contains(err.Error(), "HOLODEX_TIMEOUT_SECONDS must be positive") {
		t.Fatalf("LoadBotPlaneRuntime() error = %v, want HOLODEX_TIMEOUT_SECONDS must be positive", err)
	}
}

func TestLoadBotPlaneRuntimeRequiresHolodexAPIKey(t *testing.T) {
	t.Run("load rejects empty key", func(t *testing.T) {
		setBotPlaneEnv(t)
		t.Setenv("HOLODEX_API_KEY", "")

		_, err := LoadBotPlaneRuntime()
		if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY is required") {
			t.Fatalf("LoadBotPlaneRuntime() error = %v, want HOLODEX_API_KEY is required", err)
		}
	})

	t.Run("Validate rejects blank key", func(t *testing.T) {
		setBotPlaneEnv(t)

		config, err := LoadBotPlaneRuntime()
		if err != nil {
			t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
		}

		config.Holodex.APIKey = "   "

		err = config.Validate()
		if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY is required") {
			t.Fatalf("Validate() error = %v, want HOLODEX_API_KEY is required", err)
		}
	})
}

func TestLoadBotPlaneRuntimeUsesSeparateIrisTokens(t *testing.T) {
	setBotPlaneEnv(t)
	t.Setenv(settingstest.IrisWebhookTokenEnv, " webhook-token ")
	t.Setenv(settingstest.IrisBotTokenEnv, " bot-token ")

	config, err := LoadBotPlaneRuntime()
	if err != nil {
		t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
	}

	if config.Iris.WebhookToken != "webhook-token" || config.Iris.BotToken != "bot-token" {
		t.Fatalf("Iris tokens = (%q,%q), want trimmed separate tokens", config.Iris.WebhookToken, config.Iris.BotToken)
	}
}

func TestLoadBotPlaneRuntimeRequiresIrisEgressInputs(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want string
	}{
		{key: settingstest.IrisWebhookTokenEnv, want: "IRIS_WEBHOOK_TOKEN is required"},
		{key: settingstest.IrisBotTokenEnv, want: "IRIS_BOT_TOKEN is required"},
		{key: "KAKAO_ROOMS", want: "KAKAO_ROOMS is required"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setBotPlaneEnv(t)
			t.Setenv(tc.key, "")

			_, err := LoadBotPlaneRuntime()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadBotPlaneRuntime() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadBotPlaneRuntimeServerHTTP3Config(t *testing.T) {
	setBotPlaneEnv(t)
	t.Setenv("SERVER_PORT", "30001")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", settingstest.HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", settingstest.HololiveH3KeyPath)
	t.Setenv("HOLOLIVE_SHORT_LINK_ADDR", " 127.0.0.1:30101 ")

	config, err := LoadBotPlaneRuntime()
	if err != nil {
		t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
	}

	if got, want := config.Server.HTTPTransports, []string{"h3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Server.HTTPTransports = %#v, want %#v", got, want)
	}

	if config.Server.H3Addr != ":30001" || config.Server.H3CertFile != settingstest.HololiveH3CertPath ||
		config.Server.H3KeyFile != settingstest.HololiveH3KeyPath {
		t.Fatalf("Server H3 = (%q,%q,%q)", config.Server.H3Addr, config.Server.H3CertFile, config.Server.H3KeyFile)
	}

	if config.Server.ShortLinkAddr != "127.0.0.1:30101" {
		t.Fatalf("Server.ShortLinkAddr = %q, want 127.0.0.1:30101", config.Server.ShortLinkAddr)
	}

	if !config.Server.TransportEnabled("h3") {
		t.Fatal("Server.TransportEnabled(h3) = false, want true")
	}
}

func TestLoadBotPlaneRuntimeRejectsInvalidServerTransport(t *testing.T) {
	for _, tc := range []struct {
		name       string
		transports string
		clearCerts bool
		want       string
	}{
		{name: "h3 without cert files", transports: "h3", clearCerts: true, want: "HOLOLIVE_H3_CERT_FILE is required"},
		{name: "h3 aliases without cert files", transports: "http/3,quic", clearCerts: true, want: "HOLOLIVE_H3_CERT_FILE is required"},
		{name: "unsupported value", transports: "htp3", want: "unsupported HOLOLIVE_HTTP_TRANSPORTS value: htp3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBotPlaneEnv(t)
			t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", tc.transports)

			if tc.clearCerts {
				t.Setenv("HOLOLIVE_H3_CERT_FILE", "")
				t.Setenv("HOLOLIVE_H3_KEY_FILE", "")
			}

			_, err := LoadBotPlaneRuntime()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadBotPlaneRuntime() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadBotPlaneRuntimeProductionPolicies(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "API secret key", env: map[string]string{"API_SECRET_KEY": ""}, want: "API_SECRET_KEY is required in production"},
		{name: "weak sslmode", env: map[string]string{"POSTGRES_SSLMODE": "require"}, want: "POSTGRES_SSLMODE=require is not allowed in production"},
		{
			name: "weak sslmode with retired override",
			env:  map[string]string{"POSTGRES_SSLMODE": "require", "POSTGRES_SSLMODE_ALLOW_INSECURE": "true"},
			want: "POSTGRES_SSLMODE=require is not allowed in production",
		},
		{name: "verify-ca", env: map[string]string{"POSTGRES_SSLMODE": "verify-ca"}, want: "POSTGRES_SSLMODE=verify-ca is not allowed in production"},
		{
			name: "verify-ca with retired override",
			env:  map[string]string{"POSTGRES_SSLMODE": "verify-ca", "POSTGRES_SSLMODE_ALLOW_INSECURE": "true"},
			want: "POSTGRES_SSLMODE=verify-ca is not allowed in production",
		},
		{
			name: "enforced CORS without origins",
			env:  map[string]string{"CORS_ALLOWED_ORIGINS": "", "CORS_ENFORCE": "true"},
			want: "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBotPlaneEnv(t)
			t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
			t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := LoadBotPlaneRuntime()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadBotPlaneRuntime() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadBotPlaneRuntimeProductionAllowsVerifyFullAndMonitorModeCORS(t *testing.T) {
	setBotPlaneEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", runtimepolicy.PostgresSSLModeVerifyFull)
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "false")

	config, err := LoadBotPlaneRuntime()
	if err != nil {
		t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
	}

	if config.Postgres.SSLMode != runtimepolicy.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want verify-full", config.Postgres.SSLMode)
	}
}

func TestLoadBotPlaneRuntimeDevelopmentAllowsWeakPostgresSSLMode(t *testing.T) {
	setBotPlaneEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("POSTGRES_SSLMODE", "prefer")

	config, err := LoadBotPlaneRuntime()
	if err != nil {
		t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
	}

	if config.Postgres.SSLMode != "prefer" {
		t.Fatalf("Postgres.SSLMode = %q, want prefer", config.Postgres.SSLMode)
	}
}

// bot plane이 보관하지 않는 공통 구획(LLM·CORS·사진 동기화 등)의 잘못된 값도 기동 실패로 드러나고,
// 한 번의 실패가 잘못된 키를 모두 보인다.
func TestLoadBotPlaneRuntimeRejectsInvalidCommonEnvValues(t *testing.T) {
	setBotPlaneEnv(t)

	keys := map[string]string{
		"POSTGRES_PORT":           "not-a-number",
		"CACHE_PORT":              "invalid",
		"SERVER_PORT":             "invalid",
		"MEMBER_NEWS_TEMPERATURE": "warm",
		"CORS_ENFORCE":            invalidBoolEnvValue,
		"PHOTO_SYNC_ENABLED":      invalidBoolEnvValue,
		"EXA_ENABLED":             invalidBoolEnvValue,
	}
	for key, value := range keys {
		t.Setenv(key, value)
	}

	_, err := LoadBotPlaneRuntime()
	if err == nil {
		t.Fatal("LoadBotPlaneRuntime() error = nil, want invalid env rejection")
	}

	for key := range keys {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("LoadBotPlaneRuntime() error = %v, want it to name %s", err, key)
		}
	}
}

func TestLoadBotPlaneRuntimeDerivesWebhookFromLocalStackWorkerProfile(t *testing.T) {
	setBotPlaneEnv(t)

	server := settingstest.NewIrisRuntimeDiagnosticsServer(t, settingstest.LocalStackWorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)

	config, err := LoadBotPlaneRuntime()
	if err != nil {
		t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
	}

	if config.Webhook.MaxBodyBytes != 65536 {
		t.Fatalf("Webhook.MaxBodyBytes = %d, want 65536", config.Webhook.MaxBodyBytes)
	}

	if config.Webhook.DedupTTL != 16*time.Minute || config.Webhook.DedupTimeout != 200*time.Millisecond {
		t.Fatalf("Webhook dedup = (%v,%v), want (16m,200ms)", config.Webhook.DedupTTL, config.Webhook.DedupTimeout)
	}

	if config.APIWorkerProfile == nil || config.APIWorkerProfile.Loaded.Profile.ProfileID != "hololive-api-test" {
		t.Fatalf("APIWorkerProfile = %#v, want hololive-api-test", config.APIWorkerProfile)
	}

	if config.APIWorkerProfile.Loaded.Hash == "" {
		t.Fatal("APIWorkerProfile hash is empty")
	}
}

func TestBotPlaneValidateRejectsNotificationEgressOwner(t *testing.T) {
	for _, value := range []string{runtimepolicy.NotificationEgressRoleOwner, "Owner"} {
		t.Run(value, func(t *testing.T) {
			setBotPlaneEnv(t)

			config, err := LoadBotPlaneRuntime()
			if err != nil {
				t.Fatalf("LoadBotPlaneRuntime() error = %v", err)
			}

			t.Setenv(runtimepolicy.NotificationEgressRoleEnv, value)

			if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "bot must not own proactive notification egress") {
				t.Fatalf("Validate() error = %v, want proactive egress ownership rejection", err)
			}
		})
	}
}
