// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func loadBotRuntimeConfig() (*Config, error) {
	out, err := LoadBotRuntime()
	if err != nil {
		return nil, fmt.Errorf("load bot runtime: %w", err)
	}

	return out, nil
}

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	settingstest.SetRequiredLoadEnv(t)
}

func newIrisRuntimeDiagnosticsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	return settingstest.NewIrisRuntimeDiagnosticsServer(t, body)
}

func loadTestWorkerProfileDiagnosticsJSON() string {
	return settingstest.WorkerProfileDiagnosticsJSON()
}

func localStackWorkerProfileDiagnosticsJSON() string {
	return settingstest.LocalStackWorkerProfileDiagnosticsJSON()
}

func testURLHostname(t *testing.T, raw string) string {
	t.Helper()

	return settingstest.URLHostname(t, raw)
}

func TestResolveHolodexAPIKey(t *testing.T) {
	t.Run("reads only HOLODEX_API_KEY", func(t *testing.T) {
		t.Setenv("HOLODEX_API_KEY", " primary-key ")
		settingstest.UnsetEnv(t, "HOLODEX_API_KEY_1")

		got, err := envload.HolodexAPIKey()
		if err != nil {
			t.Fatalf("envload.HolodexAPIKey() error = %v", err)
		}

		if got != "primary-key" {
			t.Fatalf("envload.HolodexAPIKey() = %q, want %q", got, "primary-key")
		}
	})

	// HOLODEX_API_KEY_1은 정본이 비어도 대신 읽지 않고, 빈 값이어도 존재만으로 거절한다.
	for _, value := range []string{"", "legacy-key"} {
		t.Run("rejects retired HOLODEX_API_KEY_1="+value, func(t *testing.T) {
			t.Setenv("HOLODEX_API_KEY", "")
			t.Setenv("HOLODEX_API_KEY_1", value)

			if _, err := envload.HolodexAPIKey(); err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY_1") {
				t.Fatalf("envload.HolodexAPIKey() error = %v, want HOLODEX_API_KEY_1 rejection", err)
			}
		})
	}
}

func TestLoadNotificationConfigKeepsAlarmShortLinkBaseURL(t *testing.T) {
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", " https://short.holoshi.com ")

	config, err := loadNotificationConfig()
	if err != nil {
		t.Fatalf("loadNotificationConfig() error = %v", err)
	}

	if config.AlarmShortLinkBaseURL != "https://short.holoshi.com" {
		t.Fatalf("AlarmShortLinkBaseURL = %q, want trimmed configured origin", config.AlarmShortLinkBaseURL)
	}
}

func TestLoad_HolodexTimeoutMustBePositive(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLODEX_TIMEOUT_SECONDS", "0")

	_, err := loadBotRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "HOLODEX_TIMEOUT_SECONDS must be positive") {
		t.Fatalf("Load() error = %v, want HOLODEX_TIMEOUT_SECONDS must be positive", err)
	}
}

func TestLoad_HolodexTimeoutEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLODEX_TIMEOUT_SECONDS", "45")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Holodex.Timeout != 45*time.Second {
		t.Fatalf("Holodex.Timeout = %v, want %v", config.Holodex.Timeout, 45*time.Second)
	}
}

func TestLoad_HolodexAPIKeyRequired(t *testing.T) {
	t.Run("load rejects empty key", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("HOLODEX_API_KEY", "")

		_, err := loadBotRuntimeConfig()
		if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY is required") {
			t.Fatalf("Load() error = %v, want HOLODEX_API_KEY is required", err)
		}
	})

	t.Run("Validate rejects blank key", func(t *testing.T) {
		setRequiredLoadEnv(t)

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		config.Holodex.APIKey = "   "

		err = config.Validate()
		if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY is required") {
			t.Fatalf("Validate() error = %v, want HOLODEX_API_KEY is required", err)
		}
	})
}

func TestKakaoConfig_IsRoomAllowed(t *testing.T) {
	t.Run("ACL disabled allows all", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"room-a"},
			ACLEnabled: false,
		}

		if !config.IsRoomAllowed("other-room", "999") {
			t.Fatal("expected room to be allowed when ACL is disabled")
		}
	})

	t.Run("Matches by chat ID only", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"1234567890"},
			ACLEnabled: true,
		}

		if !config.IsRoomAllowed("테스트방", "1234567890") {
			t.Fatal("expected room to be allowed by chat ID")
		}

		if config.IsRoomAllowed("1234567890", "other-id") {
			t.Fatal("expected room to be denied - only chatID should be checked")
		}
	})

	t.Run("Empty chatID denies", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"테스트방"},
			ACLEnabled: true,
		}

		if config.IsRoomAllowed("테스트방", "") {
			t.Fatal("expected room to be denied when chatID is empty")
		}
	})

	t.Run("No match denies", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"allowed-room"},
			ACLEnabled: true,
		}

		if config.IsRoomAllowed("other-room", "999") {
			t.Fatal("expected room to be denied when no match exists")
		}
	})
}

func TestKakaoConfig_AddRemoveRoom(t *testing.T) {
	config := KakaoConfig{
		Rooms:      []string{"123"},
		ACLEnabled: true,
	}

	if !config.AddRoom(" 456 ") {
		t.Fatal("expected AddRoom to succeed")
	}

	if config.AddRoom("456") {
		t.Fatal("expected duplicate AddRoom to fail")
	}

	if !config.RemoveRoom(" 456 ") {
		t.Fatal("expected RemoveRoom to succeed")
	}

	if config.RemoveRoom("456") {
		t.Fatal("expected RemoveRoom to fail for non-existing room")
	}
}

func TestKakaoConfig_SnapshotACL_ReturnsCopy(t *testing.T) {
	config := KakaoConfig{
		Rooms:      []string{"a"},
		ACLEnabled: true,
	}

	enabled, _, rooms := config.SnapshotACL()
	if !enabled {
		t.Fatal("expected enabled to be true")
	}

	if len(rooms) != 1 || rooms[0] != "a" {
		t.Fatalf("unexpected rooms snapshot: %v", rooms)
	}

	rooms[0] = "mutated"

	_, _, rooms2 := config.SnapshotACL()

	if rooms2[0] != "a" {
		t.Fatalf("expected SnapshotACL to return a copy, got: %v", rooms2)
	}
}

func TestLoad_UsesSeparateIrisTokens(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv(irisWebhookTokenEnv, " webhook-token ")
	t.Setenv(irisBotTokenEnv, " bot-token ")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Iris.WebhookToken != "webhook-token" {
		t.Fatalf("WebhookToken = %q, want %q", config.Iris.WebhookToken, "webhook-token")
	}

	if config.Iris.BotToken != "bot-token" {
		t.Fatalf("BotToken = %q, want %q", config.Iris.BotToken, "bot-token")
	}
}

func TestLoad_ServerHTTP3Config(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVER_PORT", "30001")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", hololiveH3KeyPath)
	t.Setenv("HOLOLIVE_SHORT_LINK_ADDR", " 127.0.0.1:30101 ")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := config.Server.HTTPTransports, []string{"h3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Server.HTTPTransports = %#v, want %#v", got, want)
	}

	if config.Server.H3Addr != ":30001" {
		t.Fatalf("Server.H3Addr = %q, want :30001", config.Server.H3Addr)
	}

	if config.Server.H3CertFile != "/run/hololive-bot/certs/hololive-h3.crt" {
		t.Fatalf("Server.H3CertFile = %q", config.Server.H3CertFile)
	}

	if config.Server.H3KeyFile != hololiveH3KeyPath {
		t.Fatalf("Server.H3KeyFile = %q", config.Server.H3KeyFile)
	}

	if config.Server.ShortLinkAddr != "127.0.0.1:30101" {
		t.Fatalf("Server.ShortLinkAddr = %q, want 127.0.0.1:30101", config.Server.ShortLinkAddr)
	}

	if !config.ServerTransportEnabled("h3") {
		t.Fatal("ServerTransportEnabled(h3) = false, want true")
	}
}

func TestLoad_ServerHTTP3RequiresCertificateFiles(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "")

	_, err := loadBotRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_H3_CERT_FILE is required") {
		t.Fatalf("Load() error = %v, want missing H3 cert file", err)
	}
}

func TestLoad_ServerHTTP3AliasesRequireCertificateFiles(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "http/3,quic")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "")

	_, err := loadBotRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_H3_CERT_FILE is required") {
		t.Fatalf("Load() error = %v, want missing H3 cert file", err)
	}
}

func TestLoad_ServerHTTPTransportsRejectUnsupportedValue(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "htp3")

	_, err := loadBotRuntimeConfig()
	if err == nil || !strings.Contains(err.Error(), "unsupported HOLOLIVE_HTTP_TRANSPORTS value: htp3") {
		t.Fatalf("Load() error = %v, want unsupported transport", err)
	}
}

func TestLoad_CORSProductionMonitorModeAllowsMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "false")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(config.CORS.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %v, want empty", config.CORS.AllowedOrigins)
	}

	if !config.CORS.MissingInProduction {
		t.Fatal("MissingInProduction = false, want true")
	}
}

func TestLoad_UnsupportedLegacyTelemetryEnvRejected(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("OTEL_ENVIRONMENT", "development")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected unsupported legacy env error, got nil")
	}

	if !strings.Contains(err.Error(), "OTEL_ENVIRONMENT is retired") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionEnforceModeFailsWhenMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "true")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionFiltersWildcardAndLocalhost(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("CORS_ENFORCE", "false")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*,http://localhost:5173,https://admin.example.com")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := []string{"https://admin.example.com"}
	if !reflect.DeepEqual(config.CORS.AllowedOrigins, expected) {
		t.Fatalf("AllowedOrigins = %v, want %v", config.CORS.AllowedOrigins, expected)
	}
}

func TestLoad_UnsupportedLegacyDBAliasRejected(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("DB_SSLMODE", "disable")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected unsupported legacy env error, got nil")
	}

	if !strings.Contains(err.Error(), "DB_SSLMODE is retired") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_UnsupportedLegacyQueryModeAliasRejected(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("DB_QUERY_EXEC_MODE", "describe_exec")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected unsupported legacy env error, got nil")
	}

	if !strings.Contains(err.Error(), "DB_QUERY_EXEC_MODE is retired") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_LLMConfig(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		setRequiredLoadEnv(t)
	}

	t.Run("new env only", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNewsModel != "new-model" {
			t.Errorf("MemberNewsModel = %q, want %q", config.LLM.MemberNewsModel, "new-model")
		}
	})

	t.Run("old env only rejected", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_CLIPROXY_MODEL", "old-model")

		_, err := loadBotRuntimeConfig()
		if err == nil {
			t.Fatal("Load() expected unsupported legacy env error, got nil")
		}

		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is retired") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("new and old env set rejected", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")
		t.Setenv("MEMBER_NEWS_CLIPROXY_MODEL", "new-model")

		_, err := loadBotRuntimeConfig()
		if err == nil {
			t.Fatal("Load() expected unsupported legacy env error, got nil")
		}

		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is retired") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("both unset", func(t *testing.T) {
		setup(t)

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNewsModel != "" {
			t.Errorf("MemberNewsModel = %q, want empty", config.LLM.MemberNewsModel)
		}
	})

	t.Run("temperature default", func(t *testing.T) {
		setup(t)

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNewsTemperature != 0.0 {
			t.Errorf("MemberNewsTemperature = %v, want 0.0", config.LLM.MemberNewsTemperature)
		}
	})
}

func TestLoad_DefaultPostgresSSLModeVerifyFull(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLMode != runtimepolicy.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want %q", config.Postgres.SSLMode, runtimepolicy.PostgresSSLModeVerifyFull)
	}
}

func TestLoad_PostgresSSLRootCertEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_SSLMODE", runtimepolicy.PostgresSSLModeVerifyFull)
	t.Setenv("POSTGRES_SSLROOTCERT", "/run/postgresql/root.crt")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLRootCert != "/run/postgresql/root.crt" {
		t.Fatalf("Postgres.SSLRootCert = %q, want %q", config.Postgres.SSLRootCert, "/run/postgresql/root.crt")
	}
}

func TestLoad_ProductionRequiresAPISecretKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("API_SECRET_KEY", "")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production API key validation error, got nil")
	}

	if !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsWeakPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production sslmode validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(err.Error(), runtimepolicy.PostgresSSLModeVerifyFull) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsVerifyCAPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "verify-ca")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production verify-ca validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=verify-ca is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(err.Error(), runtimepolicy.PostgresSSLModeVerifyFull) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsVerifyCAPostgresSSLMode_WithRetiredOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "verify-ca")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "true")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production verify-ca validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=verify-ca is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionAllowsVerifyFullPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", runtimepolicy.PostgresSSLModeVerifyFull)
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLMode != runtimepolicy.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want verify-full", config.Postgres.SSLMode)
	}
}

func TestLoad_ProductionRejectsWeakPostgresSSLMode_WithRetiredOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "true")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production sslmode validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_DevelopmentAllowsWeakPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("POSTGRES_SSLMODE", "prefer")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLMode != "prefer" {
		t.Fatalf("Postgres.SSLMode = %q, want prefer", config.Postgres.SSLMode)
	}
}

func TestLoadLLMConfig_ConsensusDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.LLM.MemberNews.Enabled {
		t.Error("ConsensusEnabled should default to false")
	}

	if config.LLM.MemberNews.Confidence != 0.85 {
		t.Errorf("ConsensusConfidence = %v, want 0.85", config.LLM.MemberNews.Confidence)
	}

	if config.LLM.MemberNews.ReviewTimeout != 30 {
		t.Errorf("ConsensusReviewTimeout = %d, want 30", config.LLM.MemberNews.ReviewTimeout)
	}

	if config.LLM.MemberNews.AdjudicateTimeout != 45 {
		t.Errorf("ConsensusAdjudicateTimeout = %d, want 45", config.LLM.MemberNews.AdjudicateTimeout)
	}

	if config.LLM.MemberNews.ReviewerModel != "" {
		t.Errorf("ConsensusReviewerModel = %q, want empty", config.LLM.MemberNews.ReviewerModel)
	}

	if config.LLM.MemberNews.AdjudicatorModel != "" {
		t.Errorf("ConsensusAdjudicatorModel = %q, want empty", config.LLM.MemberNews.AdjudicatorModel)
	}
}

func TestLoadLLMConfig_ConsensusConfidenceClamp(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("negative clamped to 0", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "-0.5")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.Confidence != 0.0 {
			t.Errorf("ConsensusConfidence = %v, want 0.0", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("above 1 clamped to 1", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "1.5")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.Confidence != 1.0 {
			t.Errorf("ConsensusConfidence = %v, want 1.0", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("NaN falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "NaN")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("Inf falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "Inf")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", config.LLM.MemberNews.Confidence)
		}
	})
}

func TestLoadLLMConfig_ConsensusTimeoutMinimum(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("review timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEW_TIMEOUT_SEC", "2")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.ReviewTimeout != 30 {
			t.Errorf("ConsensusReviewTimeout = %d, want 30 (default on <5)", config.LLM.MemberNews.ReviewTimeout)
		}
	})

	t.Run("adjudicate timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_ADJUDICATE_TIMEOUT_SEC", "3")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.AdjudicateTimeout != 45 {
			t.Errorf("ConsensusAdjudicateTimeout = %d, want 45 (default on <5)", config.LLM.MemberNews.AdjudicateTimeout)
		}
	})
}

func TestLoadLLMConfig_ConsensusModelFallback(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("empty reviewer model falls back to MemberNewsModel", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "primary-model")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.ReviewerModel != "" {
			t.Errorf("ConsensusReviewerModel = %q, want empty (fallback at provider level)", config.LLM.MemberNews.ReviewerModel)
		}
	})

	t.Run("explicit reviewer model preserved", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEWER_MODEL", "gpt-4.1-mini")

		config, err := loadBotRuntimeConfig()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if config.LLM.MemberNews.ReviewerModel != "gpt-4.1-mini" {
			t.Errorf("ConsensusReviewerModel = %q, want gpt-4.1-mini", config.LLM.MemberNews.ReviewerModel)
		}
	})
}

func TestLoadBotConfig_MarkdownReplies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "default renders plaintext"},
		{name: "explicit plaintext", value: "false"},
		{name: "explicit native markdown", value: strconv.FormatBool(true), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOT_MARKDOWN_REPLIES", tc.value)

			config, err := loadBotConfig()
			if err != nil {
				t.Fatalf("loadBotConfig() error = %v", err)
			}

			if config.MarkdownReplies != tc.want {
				t.Fatalf("MarkdownReplies = %t, want %t", config.MarkdownReplies, tc.want)
			}
		})
	}
}

func TestLoadBotConfig_SeeMoreFold(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "default folds long lists", want: true},
		{name: "explicit kill switch", value: "false"},
		{name: "explicit fold", value: strconv.FormatBool(true), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOT_SEE_MORE_FOLD", tc.value)

			config, err := loadBotConfig()
			if err != nil {
				t.Fatalf("loadBotConfig() error = %v", err)
			}

			if config.SeeMoreFold != tc.want {
				t.Fatalf("SeeMoreFold = %t, want %t", config.SeeMoreFold, tc.want)
			}
		})
	}
}

func TestLoadBotConfig_CalendarImageCacheDir(t *testing.T) {
	t.Setenv("BOT_CALENDAR_IMAGE_CACHE_DIR", "/tmp/calendar-cache")
	t.Setenv("BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", "3600")

	config, err := loadBotConfig()
	if err != nil {
		t.Fatalf("loadBotConfig() error = %v", err)
	}

	if config.CalendarImageCacheDir != "/tmp/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want /tmp/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 1h", config.CalendarEntryCacheTTL)
	}
}

func TestLoadBotConfig_DefaultCalendarImageCacheDir(t *testing.T) {
	config, err := loadBotConfig()
	if err != nil {
		t.Fatalf("loadBotConfig() error = %v", err)
	}

	if config.CalendarImageCacheDir != "data/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want data/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != 24*time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 24h", config.CalendarEntryCacheTTL)
	}
}

// newBaseConfig 공통 구획 여러 곳의 잘못된 숫자 env는 기본값으로 바뀌지 않고, 한 번의 기동 실패가 잘못된 키를 모두 보인다.
func TestLoad_InvalidNumericEnvReportsEveryKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_PORT", "not-a-number")
	t.Setenv("CACHE_PORT", "invalid")
	t.Setenv("SERVER_PORT", "invalid")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid numeric env rejection")
	}

	for _, key := range []string{"POSTGRES_PORT", "CACHE_PORT", "SERVER_PORT"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("Load() error = %v, want it to name %s", err, key)
		}
	}
}

func TestLoad_WebhookUsesLocalStackWorkerProfile(t *testing.T) {
	setRequiredLoadEnv(t)

	server := newIrisRuntimeDiagnosticsServer(t, localStackWorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Webhook.WorkerCount != 16 {
		t.Fatalf("Webhook.WorkerCount = %d, want 16", config.Webhook.WorkerCount)
	}

	if config.Webhook.QueueSize != 0 {
		t.Fatalf("Webhook.QueueSize = %d, want unused zero value", config.Webhook.QueueSize)
	}

	if config.Webhook.EnqueueTimeout != 0 {
		t.Fatalf("Webhook.EnqueueTimeout = %v, want unused zero value", config.Webhook.EnqueueTimeout)
	}

	if config.Webhook.HandlerTimeout != 30*time.Second {
		t.Fatalf("Webhook.HandlerTimeout = %v, want 30s", config.Webhook.HandlerTimeout)
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
