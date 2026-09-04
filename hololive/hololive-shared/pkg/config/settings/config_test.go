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
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
	"github.com/kapu/hololive-shared/pkg/constants"
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

func assertScraperPoll(t *testing.T, got, want ScraperPoll) {
	t.Helper()

	if got != want {
		t.Fatalf("Scraper.Poll = %+v, want %+v", got, want)
	}
}

func TestResolveHolodexAPIKey(t *testing.T) {
	t.Run("prefers HOLODEX_API_KEY", func(t *testing.T) {
		t.Setenv("HOLODEX_API_KEY", " primary-key ")
		t.Setenv("HOLODEX_API_KEY_1", "legacy-key")

		got := load.HolodexAPIKey()
		if got != "primary-key" {
			t.Fatalf("load.HolodexAPIKey() = %q, want %q", got, "primary-key")
		}
	})

	t.Run("falls back to legacy HOLODEX_API_KEY_1", func(t *testing.T) {
		t.Setenv("HOLODEX_API_KEY", "")
		t.Setenv("HOLODEX_API_KEY_1", "legacy-key")

		got := load.HolodexAPIKey()
		if got != "legacy-key" {
			t.Fatalf("load.HolodexAPIKey() = %q, want %q", got, "legacy-key")
		}
	})
}

func TestLoadNotificationConfigKeepsAlarmShortLinkBaseURL(t *testing.T) {
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", " https://short.holoshi.com ")

	config := loadNotificationConfig()

	if config.AlarmShortLinkBaseURL != "https://short.holoshi.com" {
		t.Fatalf("AlarmShortLinkBaseURL = %q, want trimmed configured origin", config.AlarmShortLinkBaseURL)
	}
}

func assertHolodexLiveStatusFallbackConfig(t *testing.T, got, want HolodexLiveStatusFallbackConfig) {
	t.Helper()

	if got != want {
		t.Fatalf("Holodex.LiveStatusFallback = %+v, want %+v", got, want)
	}
}

func TestDefaultHolodexOperationalConfig_LiveStatusFallbackDefaults(t *testing.T) {
	config := DefaultHolodexOperationalConfig()

	assertHolodexLiveStatusFallbackConfig(t, config.LiveStatusFallback, HolodexLiveStatusFallbackConfig{
		MaxPerCycle:     4,
		WallClockBudget: 12 * time.Second,
		DeadlineMargin:  500 * time.Millisecond,
	})
}

func TestLoad_HolodexLiveStatusFallbackEnvOverrides(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE", "7")
	t.Setenv("HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS", "18")
	t.Setenv("HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS", "750")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertHolodexLiveStatusFallbackConfig(t, config.Holodex.LiveStatusFallback, HolodexLiveStatusFallbackConfig{
		MaxPerCycle:     7,
		WallClockBudget: 18 * time.Second,
		DeadlineMargin:  750 * time.Millisecond,
	})
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
	t.Run("load rejects both key env vars empty", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("HOLODEX_API_KEY", "")
		t.Setenv("HOLODEX_API_KEY_1", "")

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

func TestLoad_HolodexLiveStatusFallbackValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "rejects zero max per cycle",
			env: map[string]string{
				"HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE": "0",
			},
			wantErr: "HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE must be positive",
		},
		{
			name: "rejects zero wall clock budget",
			env: map[string]string{
				"HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS": "0",
			},
			wantErr: "HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS must be positive",
		},
		{
			name: "rejects negative deadline margin",
			env: map[string]string{
				"HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS": "-1",
			},
			wantErr: "HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredLoadEnv(t)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := loadBotRuntimeConfig()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
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

func TestLoad_CommunityShortsBigBangCutoverDefaultsZero(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !config.Ingestion.CommunityShortsBigBangCutoverAt.IsZero() {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want zero", config.Ingestion.CommunityShortsBigBangCutoverAt)
	}
}

func TestLoad_CommunityShortsBigBangCutoverEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10T01:11:12+09:00")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := time.Date(2026, time.April, 9, 16, 11, 12, 0, time.UTC)
	if !config.Ingestion.CommunityShortsBigBangCutoverAt.Equal(want) {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want %s", config.Ingestion.CommunityShortsBigBangCutoverAt, want)
	}
}

func TestLoad_CommunityShortsBigBangCutoverRejectsInvalidRFC3339(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10 01:11:12")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT must be RFC3339") {
		t.Fatalf("Load() error = %v, want RFC3339 parse error", err)
	}
}

func TestLoad_ScraperPollDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, config.Scraper.Poll, ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      2 * time.Minute,
	})
}

func TestLoad_ScraperPollEnvOverrides(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", "420")
	t.Setenv("SCRAPER_POLL_SHORTS_INTERVAL_SECONDS", "660")
	t.Setenv("SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS", "780")
	t.Setenv("SCRAPER_POLL_STATS_INTERVAL_SECONDS", "14400")
	t.Setenv("SCRAPER_POLL_LIVE_INTERVAL_SECONDS", "180")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, config.Scraper.Poll, ScraperPoll{
		Videos:    7 * time.Minute,
		Shorts:    11 * time.Minute,
		Community: 13 * time.Minute,
		Stats:     4 * time.Hour,
		Live:      3 * time.Minute,
	})
}

func TestLoad_ScraperPollIgnoresRemovedLegacyEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_VIDEOS_SECONDS", "420")
	t.Setenv("SCRAPER_SHORTS_SECONDS", "660")
	t.Setenv("SCRAPER_COMMUNITY_SECONDS", "780")
	t.Setenv("SCRAPER_STATS_SECONDS", "14400")
	t.Setenv("SCRAPER_LIVE_SECONDS", "180")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, config.Scraper.Poll, DefaultScraperPoll())
}

func TestLoad_ScraperPollCanonicalEnvWinsOverRemovedLegacyEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", "420")
	t.Setenv("SCRAPER_POLL_SHORTS_INTERVAL_SECONDS", "660")
	t.Setenv("SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS", "780")
	t.Setenv("SCRAPER_POLL_STATS_INTERVAL_SECONDS", "14400")
	t.Setenv("SCRAPER_POLL_LIVE_INTERVAL_SECONDS", "180")
	t.Setenv("SCRAPER_VIDEOS_SECONDS", "60")
	t.Setenv("SCRAPER_SHORTS_SECONDS", "60")
	t.Setenv("SCRAPER_COMMUNITY_SECONDS", "60")
	t.Setenv("SCRAPER_STATS_SECONDS", "60")
	t.Setenv("SCRAPER_LIVE_SECONDS", "60")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, config.Scraper.Poll, ScraperPoll{
		Videos:    7 * time.Minute,
		Shorts:    11 * time.Minute,
		Community: 13 * time.Minute,
		Stats:     4 * time.Hour,
		Live:      3 * time.Minute,
	})
}

func TestLoadScraperConfigRejectsInvalidPollAndWorkerCount(t *testing.T) {
	for _, key := range []string{
		"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS",
		"SCRAPER_POLL_SHORTS_INTERVAL_SECONDS",
		"SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS",
		"SCRAPER_POLL_STATS_INTERVAL_SECONDS",
		"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
		"SCRAPER_SCHEDULER_WORKER_COUNT",
	} {
		for _, value := range []string{"0", "-1", "invalid", ""} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)

				_, err := loadScraperConfig()
				if err == nil {
					t.Fatalf("loadScraperConfig() accepted %s=%q", key, value)
				}

				if !strings.Contains(err.Error(), key) {
					t.Fatalf("loadScraperConfig() error = %v, want it to name %s", err, key)
				}
			})
		}
	}
}

func TestLoad_ScraperInvalidEnvFailsLoad(t *testing.T) {
	for _, key := range []string{
		"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
		"SCRAPER_SCHEDULER_WORKER_COUNT",
	} {
		t.Run(key, func(t *testing.T) {
			setRequiredLoadEnv(t)
			t.Setenv(key, "invalid")

			_, err := loadBotRuntimeConfig()
			if err == nil {
				t.Fatalf("Load() error = nil, want %s rejection", key)
			}

			if !strings.Contains(err.Error(), "load scraper config: ") || !strings.Contains(err.Error(), key) {
				t.Fatalf("Load() error = %v, want wrapped %s rejection", err, key)
			}
		})
	}
}

func TestLoad_ScraperBackfillDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	backfill := config.Scraper.Backfill
	if backfill.Enabled {
		t.Fatal("Scraper.Backfill.Enabled = true, want false")
	}

	if !backfill.ShortsEnabled {
		t.Fatal("Scraper.Backfill.ShortsEnabled = false, want true")
	}

	if backfill.ShortsInterval != 5*time.Minute {
		t.Fatalf("Scraper.Backfill.ShortsInterval = %s, want 5m", backfill.ShortsInterval)
	}

	if !backfill.LiveEnabled {
		t.Fatal("Scraper.Backfill.LiveEnabled = false, want true")
	}

	if backfill.LiveInterval != 3*time.Minute {
		t.Fatalf("Scraper.Backfill.LiveInterval = %s, want 3m", backfill.LiveInterval)
	}

	if backfill.TargetGroup != "notification" {
		t.Fatalf("Scraper.Backfill.TargetGroup = %q, want notification", backfill.TargetGroup)
	}
}

func TestLoad_ScraperBackfillEnvOverrides(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_BACKFILL_ENABLED", "true")
	t.Setenv("SCRAPER_BACKFILL_SHORTS_ENABLED", "false")
	t.Setenv("SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS", "420")
	t.Setenv("SCRAPER_BACKFILL_LIVE_ENABLED", "false")
	t.Setenv("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS", "180")
	t.Setenv("SCRAPER_BACKFILL_TARGET_GROUP", " notification ")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	backfill := config.Scraper.Backfill
	if !backfill.Enabled {
		t.Fatal("Scraper.Backfill.Enabled = false, want true")
	}

	if backfill.ShortsEnabled {
		t.Fatal("Scraper.Backfill.ShortsEnabled = true, want false")
	}

	if backfill.ShortsInterval != 7*time.Minute {
		t.Fatalf("Scraper.Backfill.ShortsInterval = %s, want 7m", backfill.ShortsInterval)
	}

	if backfill.LiveEnabled {
		t.Fatal("Scraper.Backfill.LiveEnabled = true, want false")
	}

	if backfill.LiveInterval != 3*time.Minute {
		t.Fatalf("Scraper.Backfill.LiveInterval = %s, want 3m", backfill.LiveInterval)
	}

	if backfill.TargetGroup != "notification" {
		t.Fatalf("Scraper.Backfill.TargetGroup = %q, want notification", backfill.TargetGroup)
	}
}

func TestLoad_ScraperBackfillValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "rejects unsupported target group",
			env: map[string]string{
				"SCRAPER_BACKFILL_ENABLED":      "true",
				"SCRAPER_BACKFILL_TARGET_GROUP": "all",
			},
			wantErr: "SCRAPER_BACKFILL_TARGET_GROUP must be notification",
		},
		{
			name: "rejects enabled shorts zero interval",
			env: map[string]string{
				"SCRAPER_BACKFILL_ENABLED":                 "true",
				"SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS": "0",
				"SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS":   "180",
			},
			wantErr: "SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS must be positive when backfill shorts is enabled",
		},
		{
			name: "allows disabled backfill zero intervals",
			env: map[string]string{
				"SCRAPER_BACKFILL_ENABLED":                 "false",
				"SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS": "0",
				"SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS":   "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredLoadEnv(t)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := loadBotRuntimeConfig()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_ScraperWorkerCountEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_WORKER_COUNT", "6")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", config.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperWorkerCountIgnoresRemovedLegacyEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_WORKER_COUNT", "6")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.WorkerCount != DefaultScraperWorkerCount() {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", config.Scraper.WorkerCount, DefaultScraperWorkerCount())
	}
}

func TestLoad_ScraperWorkerCountCanonicalEnvWinsOverRemovedLegacyEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_WORKER_COUNT", "6")
	t.Setenv("SCRAPER_WORKER_COUNT", "9")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", config.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperFetcherEngineDefault(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.FetcherEngine != ScraperFetcherEngineNetHTTP {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", config.Scraper.FetcherEngine, ScraperFetcherEngineNetHTTP)
	}
}

func TestLoad_ScraperFetcherEngineRejectsRemovedGoScrapy(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "goscrapy")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want removed goscrapy engine error")
	}

	if !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE must be one of: nethttp (goscrapy has been removed)") {
		t.Fatalf("Load() error = %v, want removed goscrapy engine error", err)
	}
}

func TestLoad_ScraperFetcherEngineValidation(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "bad-engine")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid scraper fetcher engine error")
	}

	if !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE must be one of") {
		t.Fatalf("Load() error = %v, want SCRAPER_FETCHER_ENGINE validation error", err)
	}
}

func TestLoad_ScraperFetcherEngineRejectsBrowserSnapshot(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "browser_snapshot")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid scraper fetcher engine error")
	}

	if !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE must be one of: nethttp (goscrapy has been removed)") {
		t.Fatalf("Load() error = %v, want SCRAPER_FETCHER_ENGINE validation error", err)
	}
}

func TestLoad_ScraperSnapshotAndChannelHealthDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.Snapshot.Enabled {
		t.Fatal("Scraper.Snapshot.Enabled = true, want default false")
	}

	if !config.Scraper.ChannelHealth.Enabled {
		t.Fatal("Scraper.ChannelHealth.Enabled = false, want default true")
	}

	if !config.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = false, want default true")
	}

	if config.Scraper.Snapshot.MaxBodyBytes != 512<<10 {
		t.Fatalf("Scraper.Snapshot.MaxBodyBytes = %d, want %d", config.Scraper.Snapshot.MaxBodyBytes, 512<<10)
	}

	if config.Scraper.PollTiering.Enabled {
		t.Fatal("Scraper.PollTiering.Enabled = true, want default false")
	}
}

func TestLoad_ScraperChannelHealthEnforceCanBeDisabled(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_CHANNEL_HEALTH_ENFORCE", "false")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = true, want explicit false override")
	}
}

func TestLoad_ScraperSnapshotAndChannelHealthEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SNAPSHOT_ENABLED", "true")
	t.Setenv("SCRAPER_SNAPSHOT_DIR", "/tmp/snapshots")
	t.Setenv("SCRAPER_SNAPSHOT_MAX_BODY_BYTES", "1024")
	t.Setenv("SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS", "60")
	t.Setenv("SCRAPER_CHANNEL_HEALTH_ENABLED", "false")
	t.Setenv("SCRAPER_CHANNEL_HEALTH_ENFORCE", "true")
	t.Setenv("SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_BASE_SECONDS", "120")
	t.Setenv("SCRAPER_BROWSER_DIAGNOSTIC_ENABLED", "true")
	t.Setenv("SCRAPER_BROWSER_DIAGNOSTIC_ENDPOINT", "http://browser:9222/snapshot")
	t.Setenv("SCRAPER_POLL_TIERING_ENABLED", "true")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !config.Scraper.Snapshot.Enabled {
		t.Fatal("Scraper.Snapshot.Enabled = false, want true")
	}

	if config.Scraper.Snapshot.Dir != "/tmp/snapshots" {
		t.Fatalf("Scraper.Snapshot.Dir = %q", config.Scraper.Snapshot.Dir)
	}

	if config.Scraper.Snapshot.MaxBodyBytes != 1024 {
		t.Fatalf("Scraper.Snapshot.MaxBodyBytes = %d, want 1024", config.Scraper.Snapshot.MaxBodyBytes)
	}

	if config.Scraper.Snapshot.MinInterval != time.Minute {
		t.Fatalf("Scraper.Snapshot.MinInterval = %s, want 1m", config.Scraper.Snapshot.MinInterval)
	}

	if config.Scraper.ChannelHealth.Enabled {
		t.Fatal("Scraper.ChannelHealth.Enabled = true, want false")
	}

	if !config.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = false, want true")
	}

	if config.Scraper.ChannelHealth.ParserDriftBase != 2*time.Minute {
		t.Fatalf("Scraper.ChannelHealth.ParserDriftBase = %s, want 2m", config.Scraper.ChannelHealth.ParserDriftBase)
	}

	if !config.Scraper.BrowserDiagnostic.Enabled {
		t.Fatal("Scraper.BrowserDiagnostic.Enabled = false, want true")
	}

	if !config.Scraper.PollTiering.Enabled {
		t.Fatal("Scraper.PollTiering.Enabled = false, want true")
	}
}

func TestLoad_IrisSharedTokenNoLongerProvidesFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv(irisWebhookTokenEnv, "")
	t.Setenv(irisBotTokenEnv, "test-bot-token")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected missing webhook token error, got nil")
	}

	if !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionMonitorModeAllowsMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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

	if !strings.Contains(err.Error(), "OTEL_ENVIRONMENT is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CORSProductionEnforceModeFailsWhenMissingOrigins(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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

	if !strings.Contains(err.Error(), "DB_SSLMODE is no longer supported") {
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

	if !strings.Contains(err.Error(), "DB_QUERY_EXEC_MODE is no longer supported") {
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

		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is no longer supported") {
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

		if !strings.Contains(err.Error(), "MEMBER_NEWS_CLIPROXY_MODEL is no longer supported") {
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

	if config.Postgres.SSLMode != load.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want %q", config.Postgres.SSLMode, load.PostgresSSLModeVerifyFull)
	}
}

func TestLoad_PostgresSSLRootCertEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_SSLMODE", load.PostgresSSLModeVerifyFull)
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
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production sslmode validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(err.Error(), load.PostgresSSLModeVerifyFull) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsVerifyCAPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", "verify-ca")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() expected production verify-ca validation error, got nil")
	}

	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=verify-ca is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(err.Error(), load.PostgresSSLModeVerifyFull) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsVerifyCAPostgresSSLMode_WithRetiredOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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
	t.Setenv("APP_ENV", load.EnvironmentProduction)
	t.Setenv("POSTGRES_SSLMODE", load.PostgresSSLModeVerifyFull)
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLMode != load.PostgresSSLModeVerifyFull {
		t.Fatalf("Postgres.SSLMode = %q, want verify-full", config.Postgres.SSLMode)
	}
}

func TestLoad_ProductionRejectsWeakPostgresSSLMode_WithRetiredOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", load.EnvironmentProduction)
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

func TestLoadBotConfig_CalendarImageCacheDir(t *testing.T) {
	t.Setenv("BOT_CALENDAR_IMAGE_CACHE_DIR", "/tmp/calendar-cache")
	t.Setenv("BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", "3600")

	config := loadBotConfig()

	if config.CalendarImageCacheDir != "/tmp/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want /tmp/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 1h", config.CalendarEntryCacheTTL)
	}
}

func TestLoadBotConfig_DefaultCalendarImageCacheDir(t *testing.T) {
	config := loadBotConfig()

	if config.CalendarImageCacheDir != "data/calendar-cache" {
		t.Fatalf("CalendarImageCacheDir = %q, want data/calendar-cache", config.CalendarImageCacheDir)
	}

	if config.CalendarEntryCacheTTL != 24*time.Hour {
		t.Fatalf("CalendarEntryCacheTTL = %s, want 24h", config.CalendarEntryCacheTTL)
	}
}

func TestLoad_InvalidNumericStillUsesDefault(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_PORT", "not-a-number")
	t.Setenv("CACHE_PORT", "invalid")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.Port != constants.DatabaseDefaults.Port {
		t.Fatalf("Postgres.Port = %d, want %d", config.Postgres.Port, constants.DatabaseDefaults.Port)
	}

	if config.Valkey.Port != 6379 {
		t.Fatalf("Valkey.Port = %d, want %d", config.Valkey.Port, 6379)
	}
}

func TestLoad_InvalidCoreNumeric(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVER_PORT", "invalid")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Server.Port != 30001 {
		t.Fatalf("Server.Port = %d, want %d", config.Server.Port, 30001)
	}

	if config.Webhook.WorkerCount != 16 {
		t.Fatalf("Webhook.WorkerCount = %d, want %d", config.Webhook.WorkerCount, 16)
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

	if !config.Webhook.RequireHMAC {
		t.Fatal("Webhook.RequireHMAC = false, want true")
	}

	if config.APIWorkerProfile == nil || config.APIWorkerProfile.Loaded.Profile.ProfileID != "hololive-api-test" {
		t.Fatalf("APIWorkerProfile = %#v, want hololive-api-test", config.APIWorkerProfile)
	}

	if config.APIWorkerProfile.Loaded.Hash == "" {
		t.Fatal("APIWorkerProfile hash is empty")
	}
}

func TestLoad_WebhookRequireHMACEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_WEBHOOK_REQUIRE_HMAC", "true")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !config.Webhook.RequireHMAC {
		t.Fatal("Webhook.RequireHMAC = false, want true")
	}
}

func TestLoad_WebhookRequireHMACFalseFailsClosed(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_WEBHOOK_REQUIRE_HMAC", "false")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want HMAC false rejection")
	}

	if !strings.Contains(err.Error(), "IRIS_WEBHOOK_REQUIRE_HMAC=false is unsupported") {
		t.Fatalf("Load() error = %v, want HMAC false rejection", err)
	}
}

func TestLoad_BackwardCompatibleLLMServiceHealthURL(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVICES_LLM_SERVER_HEALTH_URL", "http://legacy-llm-server/health")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Services.LLMSchedulerHealthURL != "http://legacy-llm-server/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want legacy value", config.Services.LLMSchedulerHealthURL)
	}
}

func TestLoad_ScraperSchedulerDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.Scheduler.PollTimeout != 45*time.Second {
		t.Fatalf("Scraper.Scheduler.PollTimeout = %s, want %s", config.Scraper.Scheduler.PollTimeout, 45*time.Second)
	}

	if config.Scraper.Scheduler.ErrorBackoffMin != 30*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMin = %s, want %s", config.Scraper.Scheduler.ErrorBackoffMin, 30*time.Second)
	}

	if config.Scraper.Scheduler.ErrorBackoffMax != 5*time.Minute {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMax = %s, want %s", config.Scraper.Scheduler.ErrorBackoffMax, 5*time.Minute)
	}
}

func TestLoad_ScraperSchedulerEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS", "22")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS", "7")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS", "99")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.Scheduler.PollTimeout != 22*time.Second {
		t.Fatalf("Scraper.Scheduler.PollTimeout = %s, want %s", config.Scraper.Scheduler.PollTimeout, 22*time.Second)
	}

	if config.Scraper.Scheduler.ErrorBackoffMin != 7*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMin = %s, want %s", config.Scraper.Scheduler.ErrorBackoffMin, 7*time.Second)
	}

	if config.Scraper.Scheduler.ErrorBackoffMax != 99*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMax = %s, want %s", config.Scraper.Scheduler.ErrorBackoffMax, 99*time.Second)
	}
}

func TestLoad_ScraperSchedulerBackoffValidation(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS", "60")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS", "30")

	_, err := loadBotRuntimeConfig()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}

	if !strings.Contains(err.Error(), "SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS") {
		t.Fatalf("Load() error = %v", err)
	}
}
