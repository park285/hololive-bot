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
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")
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

		if !cfg.IsRoomAllowed("테스트방", "1234567890") {
			t.Fatalf("expected room to be allowed by chat ID")
		}

		if cfg.IsRoomAllowed("1234567890", "other-id") {
			t.Fatalf("expected room to be denied - only chatID should be checked")
		}
	})

	t.Run("Empty chatID denies", func(t *testing.T) {
		cfg := KakaoConfig{
			Rooms:      []string{"테스트방"},
			ACLEnabled: true,
		}

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

	enabled, _, rooms := cfg.SnapshotACL()
	if !enabled {
		t.Fatalf("expected enabled to be true")
	}
	if len(rooms) != 1 || rooms[0] != "a" {
		t.Fatalf("unexpected rooms snapshot: %v", rooms)
	}

	rooms[0] = "mutated"
	_, _, rooms2 := cfg.SnapshotACL()
	if rooms2[0] != "a" {
		t.Fatalf("expected SnapshotACL to return a copy, got: %v", rooms2)
	}
}

func TestLoad_UsesSeparateIrisTokens(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_WEBHOOK_TOKEN", " webhook-token ")
	t.Setenv("IRIS_BOT_TOKEN", " bot-token ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Iris.WebhookToken != "webhook-token" {
		t.Fatalf("WebhookToken = %q, want %q", cfg.Iris.WebhookToken, "webhook-token")
	}
	if cfg.Iris.BotToken != "bot-token" {
		t.Fatalf("BotToken = %q, want %q", cfg.Iris.BotToken, "bot-token")
	}
}

func TestLoad_ServerHTTP3Config(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVER_PORT", "30001")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c,h3")
	t.Setenv("HOLOLIVE_H2C_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.Server.HTTPTransports, []string{"h2c", "h3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Server.HTTPTransports = %#v, want %#v", got, want)
	}
	if cfg.Server.H2CAddr != ":30001" {
		t.Fatalf("Server.H2CAddr = %q, want :30001", cfg.Server.H2CAddr)
	}
	if cfg.Server.H3Addr != ":30001" {
		t.Fatalf("Server.H3Addr = %q, want :30001", cfg.Server.H3Addr)
	}
	if cfg.Server.H3CertFile != "/run/hololive-bot/certs/hololive-h3.crt" {
		t.Fatalf("Server.H3CertFile = %q", cfg.Server.H3CertFile)
	}
	if cfg.Server.H3KeyFile != "/run/hololive-bot/certs/hololive-h3.key" {
		t.Fatalf("Server.H3KeyFile = %q", cfg.Server.H3KeyFile)
	}
	if !cfg.ServerTransportEnabled("h3") {
		t.Fatal("ServerTransportEnabled(h3) = false, want true")
	}
}

func TestLoad_ServerHTTP3RequiresCertificateFiles(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_H3_CERT_FILE is required") {
		t.Fatalf("Load() error = %v, want missing H3 cert file", err)
	}
}

func TestLoad_ServerHTTP3AliasesRequireCertificateFiles(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "http/3,quic")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_H3_CERT_FILE is required") {
		t.Fatalf("Load() error = %v, want missing H3 cert file", err)
	}
}

func TestLoad_ServerHTTPTransportsRejectUnsupportedValue(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c,htp3")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "unsupported HOLOLIVE_HTTP_TRANSPORTS value: htp3") {
		t.Fatalf("Load() error = %v, want unsupported transport", err)
	}
}

func TestLoad_ServerHTTPTransportsRejectClientOnlyTransportValue(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "http2")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "unsupported HOLOLIVE_HTTP_TRANSPORTS value: http2") {
		t.Fatalf("Load() error = %v, want unsupported transport", err)
	}
}

func TestLoad_CommunityShortsBigBangFlagDefaultsFalse(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Ingestion.CommunityShortsBigBangEnabled {
		t.Fatal("Ingestion.CommunityShortsBigBangEnabled = true, want false")
	}
}

func TestLoad_CommunityShortsBigBangCutoverDefaultsZero(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Ingestion.CommunityShortsBigBangCutoverAt.IsZero() {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want zero", cfg.Ingestion.CommunityShortsBigBangCutoverAt)
	}
}

func TestLoad_CommunityShortsBigBangFlagEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Ingestion.CommunityShortsBigBangEnabled {
		t.Fatal("Ingestion.CommunityShortsBigBangEnabled = false, want true")
	}
}

func TestLoad_CommunityShortsBigBangCutoverEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10T01:11:12+09:00")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := time.Date(2026, 4, 9, 16, 11, 12, 0, time.UTC)
	if !cfg.Ingestion.CommunityShortsBigBangCutoverAt.Equal(want) {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want %s", cfg.Ingestion.CommunityShortsBigBangCutoverAt, want)
	}
}

func TestLoad_CommunityShortsBigBangCutoverRejectsInvalidRFC3339(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10 01:11:12")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT must be RFC3339") {
		t.Fatalf("Load() error = %v, want RFC3339 parse error", err)
	}
}

func TestLoad_ScraperPollDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, cfg.Scraper.Poll, ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	})
}

func TestLoad_ScraperPollEnvOverrides(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", "420")
	t.Setenv("SCRAPER_POLL_SHORTS_INTERVAL_SECONDS", "660")
	t.Setenv("SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS", "780")
	t.Setenv("SCRAPER_POLL_STATS_INTERVAL_SECONDS", "14400")
	t.Setenv("SCRAPER_POLL_LIVE_INTERVAL_SECONDS", "180")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, cfg.Scraper.Poll, ScraperPoll{
		Videos:    7 * time.Minute,
		Shorts:    11 * time.Minute,
		Community: 13 * time.Minute,
		Stats:     4 * time.Hour,
		Live:      3 * time.Minute,
	})
}

func TestLoad_ScraperPollLegacyEnvFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_VIDEOS_SECONDS", "420")
	t.Setenv("SCRAPER_SHORTS_SECONDS", "660")
	t.Setenv("SCRAPER_COMMUNITY_SECONDS", "780")
	t.Setenv("SCRAPER_STATS_SECONDS", "14400")
	t.Setenv("SCRAPER_LIVE_SECONDS", "180")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertScraperPoll(t, cfg.Scraper.Poll, ScraperPoll{
		Videos:    7 * time.Minute,
		Shorts:    11 * time.Minute,
		Community: 13 * time.Minute,
		Stats:     4 * time.Hour,
		Live:      3 * time.Minute,
	})
}

func TestLoad_ScraperWorkerCountEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_WORKER_COUNT", "6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", cfg.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperWorkerCountLegacyEnvFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_WORKER_COUNT", "6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", cfg.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperFetcherEngineDefault(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.FetcherEngine != ScraperFetcherEngineNetHTTP {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", cfg.Scraper.FetcherEngine, ScraperFetcherEngineNetHTTP)
	}
}

func TestLoad_ScraperFetcherEngineEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "goscrapy")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.FetcherEngine != ScraperFetcherEngineGoScrapy {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", cfg.Scraper.FetcherEngine, ScraperFetcherEngineGoScrapy)
	}
}

func TestLoad_ScraperFetcherEngineValidation(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "bad-engine")

	_, err := Load()
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

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid scraper fetcher engine error")
	}
	if !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE must be one of: nethttp, goscrapy") {
		t.Fatalf("Load() error = %v, want SCRAPER_FETCHER_ENGINE validation error", err)
	}
}

func TestLoad_ScraperSnapshotAndChannelHealthDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.Snapshot.Enabled {
		t.Fatal("Scraper.Snapshot.Enabled = true, want default false")
	}
	if !cfg.Scraper.ChannelHealth.Enabled {
		t.Fatal("Scraper.ChannelHealth.Enabled = false, want default true")
	}
	if cfg.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = true, want default false")
	}
	if cfg.Scraper.Snapshot.MaxBodyBytes != 512<<10 {
		t.Fatalf("Scraper.Snapshot.MaxBodyBytes = %d, want %d", cfg.Scraper.Snapshot.MaxBodyBytes, 512<<10)
	}
	if cfg.Scraper.PollTiering.Enabled {
		t.Fatal("Scraper.PollTiering.Enabled = true, want default false")
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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Scraper.Snapshot.Enabled {
		t.Fatal("Scraper.Snapshot.Enabled = false, want true")
	}
	if cfg.Scraper.Snapshot.Dir != "/tmp/snapshots" {
		t.Fatalf("Scraper.Snapshot.Dir = %q", cfg.Scraper.Snapshot.Dir)
	}
	if cfg.Scraper.Snapshot.MaxBodyBytes != 1024 {
		t.Fatalf("Scraper.Snapshot.MaxBodyBytes = %d, want 1024", cfg.Scraper.Snapshot.MaxBodyBytes)
	}
	if cfg.Scraper.Snapshot.MinInterval != time.Minute {
		t.Fatalf("Scraper.Snapshot.MinInterval = %s, want 1m", cfg.Scraper.Snapshot.MinInterval)
	}
	if cfg.Scraper.ChannelHealth.Enabled {
		t.Fatal("Scraper.ChannelHealth.Enabled = true, want false")
	}
	if !cfg.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = false, want true")
	}
	if cfg.Scraper.ChannelHealth.ParserDriftBase != 2*time.Minute {
		t.Fatalf("Scraper.ChannelHealth.ParserDriftBase = %s, want 2m", cfg.Scraper.ChannelHealth.ParserDriftBase)
	}
	if !cfg.Scraper.BrowserDiagnostic.Enabled {
		t.Fatal("Scraper.BrowserDiagnostic.Enabled = false, want true")
	}
	if !cfg.Scraper.PollTiering.Enabled {
		t.Fatal("Scraper.PollTiering.Enabled = false, want true")
	}
}

func TestLoad_ScraperPublishedAtResolverDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Scraper.PublishedAtResolver.Enabled {
		t.Fatal("Scraper.PublishedAtResolver.Enabled = false, want true")
	}
	if cfg.Scraper.PublishedAtResolver.Interval != 3*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.Interval = %s, want %s", cfg.Scraper.PublishedAtResolver.Interval, 3*time.Minute)
	}
	if cfg.Scraper.PublishedAtResolver.BatchSize != 10 {
		t.Fatalf("Scraper.PublishedAtResolver.BatchSize = %d, want %d", cfg.Scraper.PublishedAtResolver.BatchSize, 10)
	}
	if cfg.Scraper.PublishedAtResolver.MaxResolvePerRun != 1 {
		t.Fatalf("Scraper.PublishedAtResolver.MaxResolvePerRun = %d, want %d", cfg.Scraper.PublishedAtResolver.MaxResolvePerRun, 1)
	}
	if cfg.Scraper.PublishedAtResolver.MaxRunDuration != 12*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MaxRunDuration = %s, want %s", cfg.Scraper.PublishedAtResolver.MaxRunDuration, 12*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.ResolveTimeout != 10*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.ResolveTimeout = %s, want %s", cfg.Scraper.PublishedAtResolver.ResolveTimeout, 10*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.MinDetectedAge != 30*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MinDetectedAge = %s, want %s", cfg.Scraper.PublishedAtResolver.MinDetectedAge, 30*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.FailureBackoffTTL != 5*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.FailureBackoffTTL = %s, want %s", cfg.Scraper.PublishedAtResolver.FailureBackoffTTL, 5*time.Minute)
	}
}

func TestLoad_ScraperPublishedAtResolverEnvOverrides(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_ENABLED", "false")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS", "21")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE", "7")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN", "3")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS", "12")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS", "9")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS", "35")
	t.Setenv("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS", "420")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.PublishedAtResolver.Enabled {
		t.Fatal("Scraper.PublishedAtResolver.Enabled = true, want false")
	}
	if cfg.Scraper.PublishedAtResolver.Interval != 21*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.Interval = %s, want %s", cfg.Scraper.PublishedAtResolver.Interval, 21*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.BatchSize != 7 {
		t.Fatalf("Scraper.PublishedAtResolver.BatchSize = %d, want %d", cfg.Scraper.PublishedAtResolver.BatchSize, 7)
	}
	if cfg.Scraper.PublishedAtResolver.MaxResolvePerRun != 3 {
		t.Fatalf("Scraper.PublishedAtResolver.MaxResolvePerRun = %d, want %d", cfg.Scraper.PublishedAtResolver.MaxResolvePerRun, 3)
	}
	if cfg.Scraper.PublishedAtResolver.MaxRunDuration != 12*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MaxRunDuration = %s, want %s", cfg.Scraper.PublishedAtResolver.MaxRunDuration, 12*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.ResolveTimeout != 9*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.ResolveTimeout = %s, want %s", cfg.Scraper.PublishedAtResolver.ResolveTimeout, 9*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.MinDetectedAge != 35*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MinDetectedAge = %s, want %s", cfg.Scraper.PublishedAtResolver.MinDetectedAge, 35*time.Second)
	}
	if cfg.Scraper.PublishedAtResolver.FailureBackoffTTL != 7*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.FailureBackoffTTL = %s, want %s", cfg.Scraper.PublishedAtResolver.FailureBackoffTTL, 7*time.Minute)
	}
}

func TestConfigValidate_ScraperPublishedAtResolverRejectsMaxRunDurationBelowResolveTimeout(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:           30001,
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30001",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      "/run/hololive-bot/certs/hololive-h3.key",
		},
		Kakao:    KakaoConfig{Rooms: []string{"test-room"}},
		Iris:     IrisConfig{WebhookToken: "test-webhook-token", BotToken: "test-bot-token", BaseURLFile: "/tmp/iris_base_url"},
		Holodex:  HolodexConfig{APIKey: "test-key"},
		YouTube:  YouTubeConfig{APIKey: "test-youtube-key"},
		Postgres: PostgresConfig{SSLMode: "disable"},
		Scraper: ScraperConfig{
			PublishedAtResolver: ScraperPublishedAtResolverConfig{
				Enabled:           true,
				Interval:          15 * time.Second,
				BatchSize:         10,
				MaxResolvePerRun:  1,
				MaxRunDuration:    5 * time.Second,
				ResolveTimeout:    10 * time.Second,
				MinDetectedAge:    30 * time.Second,
				FailureBackoffTTL: 5 * time.Minute,
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want resolver duration validation error")
	}
	if !strings.Contains(err.Error(), "SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS must be >= SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS") {
		t.Fatalf("Validate() error = %v, want max-run-duration/resolve-timeout validation error", err)
	}
}

func TestLoad_IrisSharedTokenNoLongerProvidesFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "")
	t.Setenv("IRIS_BOT_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected missing webhook token error, got nil")
	}
	if !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
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

func TestLoad_UsesProductionWhenOnlyLegacyTelemetryEnvIsSet(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("OTEL_ENVIRONMENT", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "production")
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

func TestLoad_ProductionAllowsInsecurePostgresSSLMode_WithOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "disable")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "true")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
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
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
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
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
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
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
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

func TestLoadLLMScheduler_IrisSharedTokenNoLongerProvidesFallback(t *testing.T) {
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "")
	t.Setenv("IRIS_BOT_TOKEN", "")
	t.Setenv("API_SECRET_KEY", "test-api-key")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected missing webhook token error, got nil")
	}
	if !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
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

func TestLoad_ScraperSchedulerDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.Scheduler.PollTimeout != 45*time.Second {
		t.Fatalf("Scraper.Scheduler.PollTimeout = %s, want %s", cfg.Scraper.Scheduler.PollTimeout, 45*time.Second)
	}
	if cfg.Scraper.Scheduler.ErrorBackoffMin != 30*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMin = %s, want %s", cfg.Scraper.Scheduler.ErrorBackoffMin, 30*time.Second)
	}
	if cfg.Scraper.Scheduler.ErrorBackoffMax != 5*time.Minute {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMax = %s, want %s", cfg.Scraper.Scheduler.ErrorBackoffMax, 5*time.Minute)
	}
}

func TestLoad_ScraperSchedulerEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS", "22")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS", "7")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS", "99")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.Scheduler.PollTimeout != 22*time.Second {
		t.Fatalf("Scraper.Scheduler.PollTimeout = %s, want %s", cfg.Scraper.Scheduler.PollTimeout, 22*time.Second)
	}
	if cfg.Scraper.Scheduler.ErrorBackoffMin != 7*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMin = %s, want %s", cfg.Scraper.Scheduler.ErrorBackoffMin, 7*time.Second)
	}
	if cfg.Scraper.Scheduler.ErrorBackoffMax != 99*time.Second {
		t.Fatalf("Scraper.Scheduler.ErrorBackoffMax = %s, want %s", cfg.Scraper.Scheduler.ErrorBackoffMax, 99*time.Second)
	}
}

func TestLoad_ScraperSchedulerBackoffValidation(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS", "60")
	t.Setenv("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS", "30")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS") {
		t.Fatalf("Load() error = %v", err)
	}
}
