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

package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("API_SECRET_KEY", "test-api-key")
}

func TestResolveHolodexAPIKey(t *testing.T) {
	t.Run("prefers HOLODEX_API_KEY", func(t *testing.T) {
		t.Setenv("HOLODEX_API_KEY", " primary-key ")
		t.Setenv("HOLODEX_API_KEY_1", "legacy-key")

		got := resolveHolodexAPIKey()
		if got != "primary-key" {
			t.Fatalf("resolveHolodexAPIKey() = %q, want %q", got, "primary-key")
		}
	})

	t.Run("falls back to legacy HOLODEX_API_KEY_1", func(t *testing.T) {
		t.Setenv("HOLODEX_API_KEY", "")
		t.Setenv("HOLODEX_API_KEY_1", "legacy-key")

		got := resolveHolodexAPIKey()
		if got != "legacy-key" {
			t.Fatalf("resolveHolodexAPIKey() = %q, want %q", got, "legacy-key")
		}
	})
}

func TestKakaoConfig_IsRoomAllowed(t *testing.T) {
	t.Run("ACL disabled allows all", func(t *testing.T) {
		cfg := KakaoConfig{
			Rooms:      []string{"room-a"},
			ACLEnabled: false,
		}

		if !cfg.IsRoomAllowed("other-room", "999") {
			t.Fatalf("expected room to be allowed when ACL is disabled")
		}
	})

	t.Run("Matches by chat ID only", func(t *testing.T) {
		cfg := KakaoConfig{
			Rooms:      []string{"1234567890"},
			ACLEnabled: true,
		}

		// chatID가 일치하면 허용
		if !cfg.IsRoomAllowed("테스트방", "1234567890") {
			t.Fatalf("expected room to be allowed by chat ID")
		}

		// roomName만 일치해도 chatID가 다르면 거부
		if cfg.IsRoomAllowed("1234567890", "other-id") {
			t.Fatalf("expected room to be denied - only chatID should be checked")
		}
	})

	t.Run("Empty chatID denies", func(t *testing.T) {
		cfg := KakaoConfig{
			Rooms:      []string{"테스트방"},
			ACLEnabled: true,
		}

		// chatID가 비어있으면 거부
		if cfg.IsRoomAllowed("테스트방", "") {
			t.Fatalf("expected room to be denied when chatID is empty")
		}
	})

	t.Run("No match denies", func(t *testing.T) {
		cfg := KakaoConfig{
			Rooms:      []string{"allowed-room"},
			ACLEnabled: true,
		}

		if cfg.IsRoomAllowed("other-room", "999") {
			t.Fatalf("expected room to be denied when no match exists")
		}
	})
}

func TestKakaoConfig_AddRemoveRoom(t *testing.T) {
	cfg := KakaoConfig{
		Rooms:      []string{"123"},
		ACLEnabled: true,
	}

	if !cfg.AddRoom(" 456 ") {
		t.Fatalf("expected AddRoom to succeed")
	}
	if cfg.AddRoom("456") {
		t.Fatalf("expected duplicate AddRoom to fail")
	}

	if !cfg.RemoveRoom(" 456 ") {
		t.Fatalf("expected RemoveRoom to succeed")
	}
	if cfg.RemoveRoom("456") {
		t.Fatalf("expected RemoveRoom to fail for non-existing room")
	}
}

func TestKakaoConfig_SnapshotACL_ReturnsCopy(t *testing.T) {
	cfg := KakaoConfig{
		Rooms:      []string{"a"},
		ACLEnabled: true,
	}

	enabled, rooms := cfg.SnapshotACL()
	if !enabled {
		t.Fatalf("expected enabled to be true")
	}
	if len(rooms) != 1 || rooms[0] != "a" {
		t.Fatalf("unexpected rooms snapshot: %v", rooms)
	}

	rooms[0] = "mutated"
	_, rooms2 := cfg.SnapshotACL()
	if rooms2[0] != "a" {
		t.Fatalf("expected SnapshotACL to return a copy, got: %v", rooms2)
	}
}

func TestLoad_IrisSharedTokenFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "")
	t.Setenv("IRIS_BOT_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Iris.WebhookToken != "shared-token" {
		t.Fatalf("WebhookToken = %q, want %q", cfg.Iris.WebhookToken, "shared-token")
	}
	if cfg.Iris.BotToken != "shared-token" {
		t.Fatalf("BotToken = %q, want %q", cfg.Iris.BotToken, "shared-token")
	}
}

func TestLoad_CORSProductionMonitorModeAllowsMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.CORS.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %v, want empty", cfg.CORS.AllowedOrigins)
	}
	if !cfg.CORS.MissingInProduction {
		t.Fatalf("MissingInProduction = false, want true")
	}
}

func TestLoad_RejectsPlaceholderYouTubeAPIKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_API_KEY", "your_youtube_api_key")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected placeholder youtube api key error, got nil")
	}
	if !strings.Contains(err.Error(), "YOUTUBE_API_KEY uses placeholder value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionEnforceModeFailsWhenMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "true")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionFiltersWildcardAndLocalhost(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ENFORCE", "false")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*,http://localhost:5173,https://admin.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := []string{"https://admin.example.com"}
	if !reflect.DeepEqual(cfg.CORS.AllowedOrigins, expected) {
		t.Fatalf("AllowedOrigins = %v, want %v", cfg.CORS.AllowedOrigins, expected)
	}
}

func TestLoad_DeprecatedDBAliasRejected(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("DB_SSLMODE", "disable")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected deprecated env error, got nil")
	}
	if !strings.Contains(err.Error(), "DB_SSLMODE is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_DeprecatedQueryModeAliasRejected(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("DB_QUERY_EXEC_MODE", "describe_exec")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected deprecated env error, got nil")
	}
	if !strings.Contains(err.Error(), "DB_QUERY_EXEC_MODE is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_LLMConfig(t *testing.T) {
	// 공통 필수 env 설정
	setup := func(t *testing.T) {
		t.Helper()
		setRequiredLoadEnv(t)
	}

	t.Run("new env only", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNewsModel != "new-model" {
			t.Errorf("MemberNewsModel = %q, want %q", cfg.LLM.MemberNewsModel, "new-model")
		}
	})

	t.Run("old env only rejected", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_CLIPROXY_MODEL", "old-model")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() expected deprecated env error, got nil")
		}
		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is no longer supported") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("new and old env set rejected", func(t *testing.T) {
		setup(t)
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")
		t.Setenv("MEMBER_NEWS_CLIPROXY_MODEL", "new-model")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() expected deprecated env error, got nil")
		}
		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is no longer supported") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("both unset", func(t *testing.T) {
		setup(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNewsModel != "" {
			t.Errorf("MemberNewsModel = %q, want empty", cfg.LLM.MemberNewsModel)
		}
	})

	t.Run("temperature default", func(t *testing.T) {
		setup(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNewsTemperature != 0.0 {
			t.Errorf("MemberNewsTemperature = %v, want 0.0", cfg.LLM.MemberNewsTemperature)
		}
	})
}

func TestLoad_DefaultPostgresSSLModeRequire(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Postgres.SSLMode != "require" {
		t.Fatalf("Postgres.SSLMode = %q, want %q", cfg.Postgres.SSLMode, "require")
	}
}

func TestLoad_ProductionRequiresAPISecretKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_SECRET_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected production API key validation error, got nil")
	}
	if !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "disable")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=disable is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAdminAPI_ProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "disable")

	_, err := LoadAdminAPI()
	if err == nil {
		t.Fatal("LoadAdminAPI() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=disable is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAdminAPI_ProductionRequiresAPISecretKey(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_SECRET_KEY", "")

	_, err := LoadAdminAPI()
	if err == nil {
		t.Fatal("LoadAdminAPI() expected production API key validation error, got nil")
	}
	if !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLLMScheduler_ProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "disable")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=disable is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLLMScheduler_ProductionRequiresAPISecretKey(t *testing.T) {
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_SECRET_KEY", "")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected production API key validation error, got nil")
	}
	if !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLLMConfig_ConsensusDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.LLM.MemberNews.Enabled {
		t.Error("ConsensusEnabled should default to false")
	}
	if cfg.LLM.MemberNews.Confidence != 0.85 {
		t.Errorf("ConsensusConfidence = %v, want 0.85", cfg.LLM.MemberNews.Confidence)
	}
	if cfg.LLM.MemberNews.ReviewTimeout != 30 {
		t.Errorf("ConsensusReviewTimeout = %d, want 30", cfg.LLM.MemberNews.ReviewTimeout)
	}
	if cfg.LLM.MemberNews.AdjudicateTimeout != 45 {
		t.Errorf("ConsensusAdjudicateTimeout = %d, want 45", cfg.LLM.MemberNews.AdjudicateTimeout)
	}
	if cfg.LLM.MemberNews.ReviewerModel != "" {
		t.Errorf("ConsensusReviewerModel = %q, want empty", cfg.LLM.MemberNews.ReviewerModel)
	}
	if cfg.LLM.MemberNews.AdjudicatorModel != "" {
		t.Errorf("ConsensusAdjudicatorModel = %q, want empty", cfg.LLM.MemberNews.AdjudicatorModel)
	}
}

func TestLoadLLMConfig_ConsensusConfidenceClamp(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("negative clamped to 0", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "-0.5")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.Confidence != 0.0 {
			t.Errorf("ConsensusConfidence = %v, want 0.0", cfg.LLM.MemberNews.Confidence)
		}
	})

	t.Run("above 1 clamped to 1", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "1.5")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.Confidence != 1.0 {
			t.Errorf("ConsensusConfidence = %v, want 1.0", cfg.LLM.MemberNews.Confidence)
		}
	})

	t.Run("NaN falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "NaN")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", cfg.LLM.MemberNews.Confidence)
		}
	})

	t.Run("Inf falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "Inf")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", cfg.LLM.MemberNews.Confidence)
		}
	})
}

func TestLoadLLMConfig_ConsensusTimeoutMinimum(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("review timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEW_TIMEOUT_SEC", "2")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.ReviewTimeout != 30 {
			t.Errorf("ConsensusReviewTimeout = %d, want 30 (default on <5)", cfg.LLM.MemberNews.ReviewTimeout)
		}
	})

	t.Run("adjudicate timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_ADJUDICATE_TIMEOUT_SEC", "3")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.AdjudicateTimeout != 45 {
			t.Errorf("ConsensusAdjudicateTimeout = %d, want 45 (default on <5)", cfg.LLM.MemberNews.AdjudicateTimeout)
		}
	})
}

func TestLoadLLMConfig_ConsensusModelFallback(t *testing.T) {
	setRequiredLoadEnv(t)

	t.Run("empty reviewer model falls back to MemberNewsModel", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "primary-model")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		// config 레벨에서는 빈값 유지, provider 레벨에서 fallback
		if cfg.LLM.MemberNews.ReviewerModel != "" {
			t.Errorf("ConsensusReviewerModel = %q, want empty (fallback at provider level)", cfg.LLM.MemberNews.ReviewerModel)
		}
	})

	t.Run("explicit reviewer model preserved", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEWER_MODEL", "gpt-4.1-mini")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.LLM.MemberNews.ReviewerModel != "gpt-4.1-mini" {
			t.Errorf("ConsensusReviewerModel = %q, want gpt-4.1-mini", cfg.LLM.MemberNews.ReviewerModel)
		}
	})
}

func TestLoadAdminAPI_EnvApplied(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("ADMIN_API_PORT", "39002")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if cfg.Server.Port != 39002 {
		t.Fatalf("Server.Port = %d, want %d", cfg.Server.Port, 39002)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
}

func TestLoadAdminAPI_CORSLooseBoolParsing(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("CORS_ENFORCE", "yes")

	cfg, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if !cfg.CORS.Enforce {
		t.Fatal("CORS.Enforce = false, want true")
	}
}

func TestLoadLLMScheduler_EnvApplied(t *testing.T) {
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("LLM_SCHEDULER_PORT", "39003")
	t.Setenv("BOT_PREFIX", "#")

	cfg, err := LoadLLMScheduler()
	if err != nil {
		t.Fatalf("LoadLLMScheduler() error = %v", err)
	}
	if cfg.Server.Port != 39003 {
		t.Fatalf("Server.Port = %d, want %d", cfg.Server.Port, 39003)
	}
	if cfg.Bot.Prefix != "#" {
		t.Fatalf("Bot.Prefix = %q, want %q", cfg.Bot.Prefix, "#")
	}
}

func TestLoad_InvalidNumericStillUsesDefault(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_PORT", "not-a-number")
	t.Setenv("CACHE_PORT", "invalid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Postgres.Port != constants.DatabaseDefaults.Port {
		t.Fatalf("Postgres.Port = %d, want %d", cfg.Postgres.Port, constants.DatabaseDefaults.Port)
	}
	if cfg.Valkey.Port != 6379 {
		t.Fatalf("Valkey.Port = %d, want %d", cfg.Valkey.Port, 6379)
	}
}

func TestLoad_InvalidCoreNumeric(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVER_PORT", "invalid")
	t.Setenv("WEBHOOK_WORKER_COUNT", "NaN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 30001 {
		t.Fatalf("Server.Port = %d, want %d", cfg.Server.Port, 30001)
	}
	if cfg.Webhook.WorkerCount != 16 {
		t.Fatalf("Webhook.WorkerCount = %d, want %d", cfg.Webhook.WorkerCount, 16)
	}
}

func TestLoad_BackwardCompatibleLLMServiceHealthURL(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVICES_LLM_SERVER_HEALTH_URL", "http://legacy-llm-server/health")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Services.LLMSchedulerHealthURL != "http://legacy-llm-server/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want legacy value", cfg.Services.LLMSchedulerHealthURL)
	}
}

func TestLoadAdminAPI_BackwardCompatibleLLMServiceHealthURL(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("SERVICES_LLM_SERVER_HEALTH_URL", "http://legacy-llm-server/health")

	cfg, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if cfg.Services.LLMSchedulerHealthURL != "http://legacy-llm-server/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want legacy value", cfg.Services.LLMSchedulerHealthURL)
	}
}

func TestLoad_WebhookRequireHTTP2(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("WEBHOOK_REQUIRE_HTTP2", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Webhook.RequireHTTP2 {
		t.Fatal("Webhook.RequireHTTP2 = false, want true")
	}
}
