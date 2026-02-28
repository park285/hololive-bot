package config

import (
	"reflect"
	"strings"
	"testing"
)

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY_1", "test-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
}

func TestCollectAPIKeys(t *testing.T) {
	prefix := "HOLODEX_API_KEY_"

	t.Setenv("HOLODEX_API_KEY_1", " key-1 ")
	t.Setenv("HOLODEX_API_KEY_2", "key-2")
	t.Setenv("HOLODEX_API_KEY_3", "key-3")
	t.Setenv("HOLODEX_API_KEY_4", "key-4")
	t.Setenv("HOLODEX_API_KEY_5", "key-5")
	t.Setenv("HOLODEX_API_KEYS", "key-2,key-6 , key-7")

	keys := collectAPIKeys(prefix)

	expected := []string{"key-1", "key-2", "key-3", "key-4", "key-5", "key-6", "key-7"}
	if !reflect.DeepEqual(keys, expected) {
		t.Fatalf("collectAPIKeys() = %v, expected %v", keys, expected)
	}
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
	t.Setenv("OTEL_ENVIRONMENT", "production")
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

func TestLoad_CORSProductionEnforceModeFailsWhenMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("OTEL_ENVIRONMENT", "production")
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
	t.Setenv("OTEL_ENVIRONMENT", "production")
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

func TestLoad_MajorEventScraperConfigDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.MajorEvent.ScraperEnabled {
		t.Fatalf("MajorEvent.ScraperEnabled = false, want true")
	}
	if cfg.MajorEvent.ScrapeHourKST != 6 {
		t.Fatalf("MajorEvent.ScrapeHourKST = %d, want 6", cfg.MajorEvent.ScrapeHourKST)
	}
}

func TestLoad_MajorEventScraperConfigEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("MAJOREVENT_SCRAPER_ENABLED", "false")
	t.Setenv("MAJOREVENT_SCRAPE_HOUR_KST", "4")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MajorEvent.ScraperEnabled {
		t.Fatalf("MajorEvent.ScraperEnabled = true, want false")
	}
	if cfg.MajorEvent.ScrapeHourKST != 4 {
		t.Fatalf("MajorEvent.ScrapeHourKST = %d, want 4", cfg.MajorEvent.ScrapeHourKST)
	}
}

func TestLoad_MajorEventScraperConfigInvalidHourFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("MAJOREVENT_SCRAPE_HOUR_KST", "99")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MajorEvent.ScrapeHourKST != 6 {
		t.Fatalf("MajorEvent.ScrapeHourKST = %d, want fallback 6", cfg.MajorEvent.ScrapeHourKST)
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
