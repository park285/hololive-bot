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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestResolveHolodexAPIKey(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", " primary-key ")

	if got := envload.HolodexAPIKey(); got != "primary-key" {
		t.Fatalf("envload.HolodexAPIKey() = %q, want %q", got, "primary-key")
	}
}

func TestLoadNotificationConfigKeepsAlarmShortLinkBaseURL(t *testing.T) {
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", " https://short.holoshi.com ")

	config, err := LoadNotificationConfig()
	if err != nil {
		t.Fatalf("LoadNotificationConfig() error = %v", err)
	}

	if config.AlarmShortLinkBaseURL != "https://short.holoshi.com" {
		t.Fatalf("AlarmShortLinkBaseURL = %q, want trimmed configured origin", config.AlarmShortLinkBaseURL)
	}
}

func TestLoadHolodexConfigTimeoutEnvOverride(t *testing.T) {
	t.Setenv("HOLODEX_TIMEOUT_SECONDS", "45")

	config, err := LoadHolodexConfig()
	if err != nil {
		t.Fatalf("LoadHolodexConfig() error = %v", err)
	}

	if config.Timeout != 45*time.Second {
		t.Fatalf("Holodex.Timeout = %v, want %v", config.Timeout, 45*time.Second)
	}
}

func TestValidateHolodexConfigRejectsNonPositiveTimeout(t *testing.T) {
	config := DefaultHolodexOperationalConfig()

	config.Timeout = 0

	if err := ValidateHolodexConfig(&config); err == nil || !strings.Contains(err.Error(), "HOLODEX_TIMEOUT_SECONDS must be positive") {
		t.Fatalf("ValidateHolodexConfig() error = %v, want HOLODEX_TIMEOUT_SECONDS must be positive", err)
	}
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

func TestLoadIrisConfigUsesSeparateTrimmedTokens(t *testing.T) {
	t.Setenv(irisWebhookTokenEnv, " webhook-token ")
	t.Setenv(irisBotTokenEnv, " bot-token ")

	config, err := LoadIrisConfig()
	if err != nil {
		t.Fatalf("LoadIrisConfig() error = %v", err)
	}

	if config.WebhookToken != "webhook-token" || config.BotToken != "bot-token" {
		t.Fatalf("Iris tokens = (%q,%q), want trimmed separate tokens", config.WebhookToken, config.BotToken)
	}
}

func TestValidateIrisEgressInputs(t *testing.T) {
	valid := IrisConfig{BaseURL: "https://iris.example.invalid", WebhookToken: "w", BotToken: "b"}
	if err := ValidateIrisEgressInputs(&valid); err != nil {
		t.Fatalf("ValidateIrisEgressInputs(valid) error = %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*IrisConfig)
		want   string
	}{
		{name: "webhook token", mutate: func(c *IrisConfig) { c.WebhookToken = " " }, want: "IRIS_WEBHOOK_TOKEN is required"},
		{name: "bot token", mutate: func(c *IrisConfig) { c.BotToken = "" }, want: "IRIS_BOT_TOKEN is required"},
		{name: "base URL", mutate: func(c *IrisConfig) { c.BaseURL = "" }, want: "IRIS_BASE_URL or IRIS_BASE_URL_FILE is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := valid
			tc.mutate(&config)

			if err := ValidateIrisEgressInputs(&config); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateIrisEgressInputs() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadCORSConfigProductionMonitorModeAllowsMissingOrigins(t *testing.T) {
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("CORS_ENFORCE", "false")

	config, err := LoadCORSConfig(true)
	if err != nil {
		t.Fatalf("LoadCORSConfig() error = %v", err)
	}

	if len(config.AllowedOrigins) != 0 || !config.MissingInProduction {
		t.Fatalf("CORS = %#v, want empty origins marked missing in production", config)
	}

	if err := ValidateCORSConfig(runtimepolicy.EnvironmentProduction, config); err != nil {
		t.Fatalf("ValidateCORSConfig(monitor mode) error = %v", err)
	}
}

func TestLoadCORSConfigDefaultEnforceIsRuntimeOwned(t *testing.T) {
	settingstest.UnsetEnv(t, "CORS_ENFORCE")

	for _, defaultEnforce := range []bool{true, false} {
		config, err := LoadCORSConfig(defaultEnforce)
		if err != nil {
			t.Fatalf("LoadCORSConfig(%t) error = %v", defaultEnforce, err)
		}

		if config.Enforce != defaultEnforce {
			t.Fatalf("LoadCORSConfig(%t).Enforce = %t", defaultEnforce, config.Enforce)
		}
	}
}

func TestValidateCORSConfigProductionEnforceRequiresOrigins(t *testing.T) {
	err := ValidateCORSConfig(runtimepolicy.EnvironmentProduction, CORSConfig{Enforce: true})
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true") {
		t.Fatalf("ValidateCORSConfig() error = %v, want missing origins rejection", err)
	}
}

func TestLoadCORSConfigProductionFiltersWildcardAndLocalhost(t *testing.T) {
	t.Setenv("APP_ENV", runtimepolicy.EnvironmentProduction)
	t.Setenv("CORS_ALLOWED_ORIGINS", "*,http://localhost:5173,https://admin.example.com")

	config, err := LoadCORSConfig(false)
	if err != nil {
		t.Fatalf("LoadCORSConfig() error = %v", err)
	}

	expected := []string{"https://admin.example.com"}
	if !reflect.DeepEqual(config.AllowedOrigins, expected) {
		t.Fatalf("AllowedOrigins = %v, want %v", config.AllowedOrigins, expected)
	}
}

func TestLoadLLMConfigMemberNewsModel(t *testing.T) {
	t.Run("configured model", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNewsModel != "new-model" {
			t.Errorf("MemberNewsModel = %q, want %q", llm.MemberNewsModel, "new-model")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "")
		t.Setenv("MEMBER_NEWS_TEMPERATURE", "")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNewsModel != "" || llm.MemberNewsTemperature != 0.0 {
			t.Errorf("LLM = (%q,%v), want empty model and 0 temperature", llm.MemberNewsModel, llm.MemberNewsTemperature)
		}
	})
}

func TestLoadPostgresConfigSSLDefaults(t *testing.T) {
	t.Setenv("POSTGRES_SSLMODE", "")
	t.Setenv("POSTGRES_SSLROOTCERT", "/run/postgresql/root.crt")

	config, err := LoadPostgresConfig()
	if err != nil {
		t.Fatalf("LoadPostgresConfig() error = %v", err)
	}

	if config.SSLMode != runtimepolicy.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want %q", config.SSLMode, runtimepolicy.PostgresSSLModeVerifyFull)
	}

	if config.SSLRootCert != "/run/postgresql/root.crt" {
		t.Fatalf("Postgres.SSLRootCert = %q, want %q", config.SSLRootCert, "/run/postgresql/root.crt")
	}
}

func TestValidateServerRuntime(t *testing.T) {
	valid := func() ServerConfig {
		return ServerConfig{
			Port:           30001,
			APIKey:         "key",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30001",
			H3CertFile:     settingstest.HololiveH3CertPath,
			H3KeyFile:      hololiveH3KeyPath,
		}
	}

	settingstest.ClearRuntimeRoleEnv(t)

	server := valid()
	if err := ValidateServerRuntime(runtimepolicy.EnvironmentProduction, &server); err != nil {
		t.Fatalf("ValidateServerRuntime(valid) error = %v", err)
	}

	server.APIKey = ""
	if err := ValidateServerRuntime(runtimepolicy.EnvironmentProduction, &server); err == nil || !strings.Contains(err.Error(), "API_SECRET_KEY is required in production") {
		t.Fatalf("ValidateServerRuntime(no API key) error = %v, want production API key rejection", err)
	}

	server = valid()
	server.Port = 0

	if err := ValidateServerRuntime(runtimepolicy.EnvironmentProduction, &server); err == nil || !strings.Contains(err.Error(), "SERVER_PORT is required") {
		t.Fatalf("ValidateServerRuntime(no port) error = %v, want SERVER_PORT is required", err)
	}
}

func TestLoadLLMConfig_ConsensusDefaults(t *testing.T) {
	llm, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig() error = %v", err)
	}

	if llm.MemberNews.Enabled {
		t.Error("ConsensusEnabled should default to false")
	}

	if llm.MemberNews.Confidence != 0.85 {
		t.Errorf("ConsensusConfidence = %v, want 0.85", llm.MemberNews.Confidence)
	}

	if llm.MemberNews.ReviewTimeout != 30 {
		t.Errorf("ConsensusReviewTimeout = %d, want 30", llm.MemberNews.ReviewTimeout)
	}

	if llm.MemberNews.AdjudicateTimeout != 45 {
		t.Errorf("ConsensusAdjudicateTimeout = %d, want 45", llm.MemberNews.AdjudicateTimeout)
	}

	if llm.MemberNews.ReviewerModel != "" {
		t.Errorf("ConsensusReviewerModel = %q, want empty", llm.MemberNews.ReviewerModel)
	}

	if llm.MemberNews.AdjudicatorModel != "" {
		t.Errorf("ConsensusAdjudicatorModel = %q, want empty", llm.MemberNews.AdjudicatorModel)
	}
}

func TestLoadLLMConfig_ConsensusConfidenceClamp(t *testing.T) {
	t.Run("negative clamped to 0", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "-0.5")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.0 {
			t.Errorf("ConsensusConfidence = %v, want 0.0", llm.MemberNews.Confidence)
		}
	})

	t.Run("above 1 clamped to 1", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "1.5")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 1.0 {
			t.Errorf("ConsensusConfidence = %v, want 1.0", llm.MemberNews.Confidence)
		}
	})

	t.Run("NaN falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "NaN")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", llm.MemberNews.Confidence)
		}
	})

	t.Run("Inf falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "Inf")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", llm.MemberNews.Confidence)
		}
	})
}

func TestLoadLLMConfig_ConsensusTimeoutMinimum(t *testing.T) {
	t.Run("review timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEW_TIMEOUT_SEC", "2")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewTimeout != 30 {
			t.Errorf("ConsensusReviewTimeout = %d, want 30 (default on <5)", llm.MemberNews.ReviewTimeout)
		}
	})

	t.Run("adjudicate timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_ADJUDICATE_TIMEOUT_SEC", "3")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.AdjudicateTimeout != 45 {
			t.Errorf("ConsensusAdjudicateTimeout = %d, want 45 (default on <5)", llm.MemberNews.AdjudicateTimeout)
		}
	})
}

func TestLoadLLMConfig_ConsensusModelFallback(t *testing.T) {
	t.Run("empty reviewer model falls back to MemberNewsModel", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "primary-model")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewerModel != "" {
			t.Errorf("ConsensusReviewerModel = %q, want empty (fallback at provider level)", llm.MemberNews.ReviewerModel)
		}
	})

	t.Run("explicit reviewer model preserved", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEWER_MODEL", "gpt-4.1-mini")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewerModel != "gpt-4.1-mini" {
			t.Errorf("ConsensusReviewerModel = %q, want gpt-4.1-mini", llm.MemberNews.ReviewerModel)
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

			config, err := LoadBotConfig()
			if err != nil {
				t.Fatalf("LoadBotConfig() error = %v", err)
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

			config, err := LoadBotConfig()
			if err != nil {
				t.Fatalf("LoadBotConfig() error = %v", err)
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

	config, err := LoadBotConfig()
	if err != nil {
		t.Fatalf("LoadBotConfig() error = %v", err)
	}

	if config.CalendarImageCacheDir != "/tmp/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want /tmp/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 1h", config.CalendarEntryCacheTTL)
	}
}

func TestLoadBotConfig_DefaultCalendarImageCacheDir(t *testing.T) {
	config, err := LoadBotConfig()
	if err != nil {
		t.Fatalf("LoadBotConfig() error = %v", err)
	}

	if config.CalendarImageCacheDir != "data/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want data/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != 24*time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 24h", config.CalendarEntryCacheTTL)
	}
}
