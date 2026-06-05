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
	"bytes"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/shared-go/pkg/workerconfig"
)

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	server := newIrisRuntimeDiagnosticsServer(t, loadTestWorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testURLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")
}

func captureSlogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return &output
}

var (
	irisRuntimeDiagnosticsTLSOnce     sync.Once
	irisRuntimeDiagnosticsTLSCert     tls.Certificate
	irisRuntimeDiagnosticsTLSCertFile string
	irisRuntimeDiagnosticsTLSErr      error
)

func newIrisRuntimeDiagnosticsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	cert, certFile := irisRuntimeDiagnosticsTLS(t)
	t.Setenv("SSL_CERT_FILE", certFile)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/diagnostics/runtime" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func irisRuntimeDiagnosticsTLS(t *testing.T) (tls.Certificate, string) {
	t.Helper()

	irisRuntimeDiagnosticsTLSOnce.Do(func() {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		if len(server.TLS.Certificates) == 0 {
			irisRuntimeDiagnosticsTLSErr = errors.New("test TLS server did not provide a certificate")
			return
		}

		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: server.Certificate().Raw,
		})
		file, err := os.CreateTemp("", "hololive-iris-diagnostics-ca-*.pem")
		if err != nil {
			irisRuntimeDiagnosticsTLSErr = err
			return
		}
		if _, err := file.Write(certPEM); err != nil {
			irisRuntimeDiagnosticsTLSErr = err
			_ = file.Close()
			return
		}
		if err := file.Close(); err != nil {
			irisRuntimeDiagnosticsTLSErr = err
			return
		}
		irisRuntimeDiagnosticsTLSCert = server.TLS.Certificates[0]
		irisRuntimeDiagnosticsTLSCertFile = file.Name()
	})
	if irisRuntimeDiagnosticsTLSErr != nil {
		t.Fatalf("initialize Iris diagnostics TLS failed: %v", irisRuntimeDiagnosticsTLSErr)
	}
	return irisRuntimeDiagnosticsTLSCert, irisRuntimeDiagnosticsTLSCertFile
}

func testURLHostname(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q failed: %v", raw, err)
	}
	return parsed.Hostname()
}

func writeIrisBaseURLFile(t *testing.T, raw string) string {
	t.Helper()

	path := t.TempDir() + "/iris_base_url"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write IRIS_BASE_URL_FILE failed: %v", err)
	}
	return path
}

func loadTestWorkerProfileDiagnosticsJSON() string {
	return `{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": true,
					"workerProfile": {
						"version": 1,
						"profile_id": "hololive-test",
						"delivery": {
							"lane_workers": 32,
							"lane_queue_capacity": 128,
							"max_global_in_flight": 32,
							"max_per_endpoint_in_flight": 8,
							"max_drain_per_tick": 128,
							"max_attempts": 6,
							"request_timeout_ms": 30000,
							"lane_idle_timeout_ms": 750
						},
						"receive": {
							"workers": 16,
							"queue_size": 1000,
							"enqueue_timeout_ms": 50,
							"handler_timeout_ms": 30000,
							"max_body_bytes": 65536,
							"dedup_ttl_ms": 60000,
							"dedup_timeout_ms": 200
						},
						"validation": {
							"min_queue_per_endpoint_multiplier": 4,
							"require_receive_capacity_for_endpoint_burst": true
						}
					}
				}
			}
		}
	}`
}

func TestResolveIrisBaseURLValidatesFileURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "rejects http scheme",
			raw:     "http://attacker/",
			wantErr: "https",
		},
		{
			name: "rejects host mismatch",
			raw:  "https://evil.example/",
			env: map[string]string{
				"IRIS_H3_SERVER_NAME": "otherhost",
			},
			wantErr: "must match IRIS_H3_SERVER_NAME or IRIS_BASE_URL_ALLOWED_HOSTS",
		},
		{
			name: "accepts allowed host",
			raw:  "https://100.100.1.5:3001",
			env: map[string]string{
				"IRIS_BASE_URL_ALLOWED_HOSTS": "100.100.1.5",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("IRIS_H3_SERVER_NAME", "")
			t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", "")
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			got, err := resolveIrisBaseURL(IrisConfig{BaseURLFile: writeIrisBaseURLFile(t, tt.raw)})
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveIrisBaseURL() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveIrisBaseURL() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveIrisBaseURL() error = %v", err)
			}
			if got != tt.raw {
				t.Fatalf("resolveIrisBaseURL() = %q, want %q", got, tt.raw)
			}
		})
	}
}

func TestResolveIrisBaseURLAllowsUnconfiguredHostWithWarning(t *testing.T) {
	t.Setenv("IRIS_H3_SERVER_NAME", "")
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", "")
	output := captureSlogOutput(t)

	raw := "https://iris.example:3001"
	got, err := resolveIrisBaseURL(IrisConfig{BaseURLFile: writeIrisBaseURLFile(t, raw)})
	if err != nil {
		t.Fatalf("resolveIrisBaseURL() error = %v", err)
	}
	if got != raw {
		t.Fatalf("resolveIrisBaseURL() = %q, want %q", got, raw)
	}
	for _, want := range []string{"IRIS_BASE_URL_FILE host is unvalidated", "allowlist_env"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("warning output = %q, want %q", output.String(), want)
		}
	}
}

func TestResolveIrisBaseURLValidatesDirectBaseURL(t *testing.T) {
	_, err := resolveIrisBaseURL(IrisConfig{BaseURL: "http://100.100.1.5:3001"})
	if err == nil {
		t.Fatal("resolveIrisBaseURL() error = nil, want bad scheme rejection")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatalf("resolveIrisBaseURL() error = %v, want https rejection", err)
	}
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
		config := KakaoConfig{
			Rooms:      []string{"room-a"},
			ACLEnabled: false,
		}

		if !config.IsRoomAllowed("other-room", "999") {
			t.Fatalf("expected room to be allowed when ACL is disabled")
		}
	})

	t.Run("Matches by chat ID only", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"1234567890"},
			ACLEnabled: true,
		}

		if !config.IsRoomAllowed("테스트방", "1234567890") {
			t.Fatalf("expected room to be allowed by chat ID")
		}

		if config.IsRoomAllowed("1234567890", "other-id") {
			t.Fatalf("expected room to be denied - only chatID should be checked")
		}
	})

	t.Run("Empty chatID denies", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"테스트방"},
			ACLEnabled: true,
		}

		if config.IsRoomAllowed("테스트방", "") {
			t.Fatalf("expected room to be denied when chatID is empty")
		}
	})

	t.Run("No match denies", func(t *testing.T) {
		config := KakaoConfig{
			Rooms:      []string{"allowed-room"},
			ACLEnabled: true,
		}

		if config.IsRoomAllowed("other-room", "999") {
			t.Fatalf("expected room to be denied when no match exists")
		}
	})
}

func TestKakaoConfig_AddRemoveRoom(t *testing.T) {
	config := KakaoConfig{
		Rooms:      []string{"123"},
		ACLEnabled: true,
	}

	if !config.AddRoom(" 456 ") {
		t.Fatalf("expected AddRoom to succeed")
	}
	if config.AddRoom("456") {
		t.Fatalf("expected duplicate AddRoom to fail")
	}

	if !config.RemoveRoom(" 456 ") {
		t.Fatalf("expected RemoveRoom to succeed")
	}
	if config.RemoveRoom("456") {
		t.Fatalf("expected RemoveRoom to fail for non-existing room")
	}
}

func TestKakaoConfig_SnapshotACL_ReturnsCopy(t *testing.T) {
	config := KakaoConfig{
		Rooms:      []string{"a"},
		ACLEnabled: true,
	}

	enabled, _, rooms := config.SnapshotACL()
	if !enabled {
		t.Fatalf("expected enabled to be true")
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
	t.Setenv("IRIS_WEBHOOK_TOKEN", " webhook-token ")
	t.Setenv("IRIS_BOT_TOKEN", " bot-token ")

	config, err := Load()
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
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c,h3")
	t.Setenv("HOLOLIVE_H2C_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_ADDR", ":30001")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := config.Server.HTTPTransports, []string{"h2c", "h3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Server.HTTPTransports = %#v, want %#v", got, want)
	}
	if config.Server.H2CAddr != ":30001" {
		t.Fatalf("Server.H2CAddr = %q, want :30001", config.Server.H2CAddr)
	}
	if config.Server.H3Addr != ":30001" {
		t.Fatalf("Server.H3Addr = %q, want :30001", config.Server.H3Addr)
	}
	if config.Server.H3CertFile != "/run/hololive-bot/certs/hololive-h3.crt" {
		t.Fatalf("Server.H3CertFile = %q", config.Server.H3CertFile)
	}
	if config.Server.H3KeyFile != "/run/hololive-bot/certs/hololive-h3.key" {
		t.Fatalf("Server.H3KeyFile = %q", config.Server.H3KeyFile)
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Ingestion.CommunityShortsBigBangEnabled {
		t.Fatal("Ingestion.CommunityShortsBigBangEnabled = true, want false")
	}
}

func TestLoad_CommunityShortsBigBangCutoverDefaultsZero(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !config.Ingestion.CommunityShortsBigBangCutoverAt.IsZero() {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want zero", config.Ingestion.CommunityShortsBigBangCutoverAt)
	}
}

func TestLoad_CommunityShortsBigBangFlagEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED", "true")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !config.Ingestion.CommunityShortsBigBangEnabled {
		t.Fatal("Ingestion.CommunityShortsBigBangEnabled = false, want true")
	}
}

func TestLoad_CommunityShortsBigBangCutoverEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10T01:11:12+09:00")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := time.Date(2026, 4, 9, 16, 11, 12, 0, time.UTC)
	if !config.Ingestion.CommunityShortsBigBangCutoverAt.Equal(want) {
		t.Fatalf("Ingestion.CommunityShortsBigBangCutoverAt = %s, want %s", config.Ingestion.CommunityShortsBigBangCutoverAt, want)
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

	config, err := Load()
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

	config, err := Load()
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

func TestLoad_ScraperPollLegacyEnvFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_VIDEOS_SECONDS", "420")
	t.Setenv("SCRAPER_SHORTS_SECONDS", "660")
	t.Setenv("SCRAPER_COMMUNITY_SECONDS", "780")
	t.Setenv("SCRAPER_STATS_SECONDS", "14400")
	t.Setenv("SCRAPER_LIVE_SECONDS", "180")

	config, err := Load()
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

func TestLoad_ScraperBackfillDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := Load()
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
	if !backfill.CommunityEnabled {
		t.Fatal("Scraper.Backfill.CommunityEnabled = false, want true")
	}
	if backfill.CommunityInterval != 10*time.Minute {
		t.Fatalf("Scraper.Backfill.CommunityInterval = %s, want 10m", backfill.CommunityInterval)
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
	t.Setenv("SCRAPER_BACKFILL_COMMUNITY_ENABLED", "false")
	t.Setenv("SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS", "660")
	t.Setenv("SCRAPER_BACKFILL_LIVE_ENABLED", "false")
	t.Setenv("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS", "180")
	t.Setenv("SCRAPER_BACKFILL_TARGET_GROUP", " notification ")

	config, err := Load()
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
	if backfill.CommunityEnabled {
		t.Fatal("Scraper.Backfill.CommunityEnabled = true, want false")
	}
	if backfill.CommunityInterval != 11*time.Minute {
		t.Fatalf("Scraper.Backfill.CommunityInterval = %s, want 11m", backfill.CommunityInterval)
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
				"SCRAPER_BACKFILL_ENABLED":                    "true",
				"SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS":    "0",
				"SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS": "600",
				"SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS":      "180",
			},
			wantErr: "SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS must be positive when backfill shorts is enabled",
		},
		{
			name: "allows disabled backfill zero intervals",
			env: map[string]string{
				"SCRAPER_BACKFILL_ENABLED":                    "false",
				"SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS":    "0",
				"SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS": "0",
				"SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS":      "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredLoadEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := Load()
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", config.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperWorkerCountLegacyEnvFallback(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_WORKER_COUNT", "6")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.WorkerCount != 6 {
		t.Fatalf("Scraper.WorkerCount = %d, want %d", config.Scraper.WorkerCount, 6)
	}
}

func TestLoad_ScraperFetcherEngineDefault(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.FetcherEngine != ScraperFetcherEngineNetHTTP {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", config.Scraper.FetcherEngine, ScraperFetcherEngineNetHTTP)
	}
}

func TestLoad_ScraperFetcherEngineEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "goscrapy")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.FetcherEngine != ScraperFetcherEngineGoScrapy {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", config.Scraper.FetcherEngine, ScraperFetcherEngineGoScrapy)
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.Snapshot.Enabled {
		t.Fatal("Scraper.Snapshot.Enabled = true, want default false")
	}
	if !config.Scraper.ChannelHealth.Enabled {
		t.Fatal("Scraper.ChannelHealth.Enabled = false, want default true")
	}
	if config.Scraper.ChannelHealth.Enforce {
		t.Fatal("Scraper.ChannelHealth.Enforce = true, want default false")
	}
	if config.Scraper.Snapshot.MaxBodyBytes != 512<<10 {
		t.Fatalf("Scraper.Snapshot.MaxBodyBytes = %d, want %d", config.Scraper.Snapshot.MaxBodyBytes, 512<<10)
	}
	if config.Scraper.PollTiering.Enabled {
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

	config, err := Load()
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

func TestLoad_ScraperPublishedAtResolverDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !config.Scraper.PublishedAtResolver.Enabled {
		t.Fatal("Scraper.PublishedAtResolver.Enabled = false, want true")
	}
	if config.Scraper.PublishedAtResolver.Interval != 3*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.Interval = %s, want %s", config.Scraper.PublishedAtResolver.Interval, 3*time.Minute)
	}
	if config.Scraper.PublishedAtResolver.BatchSize != 10 {
		t.Fatalf("Scraper.PublishedAtResolver.BatchSize = %d, want %d", config.Scraper.PublishedAtResolver.BatchSize, 10)
	}
	if config.Scraper.PublishedAtResolver.MaxResolvePerRun != 1 {
		t.Fatalf("Scraper.PublishedAtResolver.MaxResolvePerRun = %d, want %d", config.Scraper.PublishedAtResolver.MaxResolvePerRun, 1)
	}
	if config.Scraper.PublishedAtResolver.MaxRunDuration != 12*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MaxRunDuration = %s, want %s", config.Scraper.PublishedAtResolver.MaxRunDuration, 12*time.Second)
	}
	if config.Scraper.PublishedAtResolver.ResolveTimeout != 10*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.ResolveTimeout = %s, want %s", config.Scraper.PublishedAtResolver.ResolveTimeout, 10*time.Second)
	}
	if config.Scraper.PublishedAtResolver.MinDetectedAge != 30*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MinDetectedAge = %s, want %s", config.Scraper.PublishedAtResolver.MinDetectedAge, 30*time.Second)
	}
	if config.Scraper.PublishedAtResolver.FailureBackoffTTL != 5*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.FailureBackoffTTL = %s, want %s", config.Scraper.PublishedAtResolver.FailureBackoffTTL, 5*time.Minute)
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Scraper.PublishedAtResolver.Enabled {
		t.Fatal("Scraper.PublishedAtResolver.Enabled = true, want false")
	}
	if config.Scraper.PublishedAtResolver.Interval != 21*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.Interval = %s, want %s", config.Scraper.PublishedAtResolver.Interval, 21*time.Second)
	}
	if config.Scraper.PublishedAtResolver.BatchSize != 7 {
		t.Fatalf("Scraper.PublishedAtResolver.BatchSize = %d, want %d", config.Scraper.PublishedAtResolver.BatchSize, 7)
	}
	if config.Scraper.PublishedAtResolver.MaxResolvePerRun != 3 {
		t.Fatalf("Scraper.PublishedAtResolver.MaxResolvePerRun = %d, want %d", config.Scraper.PublishedAtResolver.MaxResolvePerRun, 3)
	}
	if config.Scraper.PublishedAtResolver.MaxRunDuration != 12*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MaxRunDuration = %s, want %s", config.Scraper.PublishedAtResolver.MaxRunDuration, 12*time.Second)
	}
	if config.Scraper.PublishedAtResolver.ResolveTimeout != 9*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.ResolveTimeout = %s, want %s", config.Scraper.PublishedAtResolver.ResolveTimeout, 9*time.Second)
	}
	if config.Scraper.PublishedAtResolver.MinDetectedAge != 35*time.Second {
		t.Fatalf("Scraper.PublishedAtResolver.MinDetectedAge = %s, want %s", config.Scraper.PublishedAtResolver.MinDetectedAge, 35*time.Second)
	}
	if config.Scraper.PublishedAtResolver.FailureBackoffTTL != 7*time.Minute {
		t.Fatalf("Scraper.PublishedAtResolver.FailureBackoffTTL = %s, want %s", config.Scraper.PublishedAtResolver.FailureBackoffTTL, 7*time.Minute)
	}
}

func TestConfigValidate_ScraperPublishedAtResolverRejectsMaxRunDurationBelowResolveTimeout(t *testing.T) {
	config := &Config{
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

	err := config.Validate()
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(config.CORS.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %v, want empty", config.CORS.AllowedOrigins)
	}
	if !config.CORS.MissingInProduction {
		t.Fatalf("MissingInProduction = false, want true")
	}
}

func TestLoad_UsesProductionWhenOnlyLegacyTelemetryEnvIsSet(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("OTEL_ENVIRONMENT", "development")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", config.Environment, "production")
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := []string{"https://admin.example.com"}
	if !reflect.DeepEqual(config.CORS.AllowedOrigins, expected) {
		t.Fatalf("AllowedOrigins = %v, want %v", config.CORS.AllowedOrigins, expected)
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

		config, err := Load()
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

		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNewsModel != "" {
			t.Errorf("MemberNewsModel = %q, want empty", config.LLM.MemberNewsModel)
		}
	})

	t.Run("temperature default", func(t *testing.T) {
		setup(t)

		config, err := Load()
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

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLMode != "verify-full" {
		t.Fatalf("Postgres.SSLMode = %q, want %q", config.Postgres.SSLMode, "verify-full")
	}
}

func TestLoad_PostgresSSLRootCertEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("POSTGRES_SSLMODE", "verify-full")
	t.Setenv("POSTGRES_SSLROOTCERT", "/run/postgresql/root.crt")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Postgres.SSLRootCert != "/run/postgresql/root.crt" {
		t.Fatalf("Postgres.SSLRootCert = %q, want %q", config.Postgres.SSLRootCert, "/run/postgresql/root.crt")
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

func TestLoad_ProductionRejectsWeakPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE_ALLOW_INSECURE=true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionRejectsVerifyCAPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "verify-ca")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected production verify-ca validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=verify-ca is not allowed in production") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "verify-full") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_ProductionAllowsVerifyCAPostgresSSLMode_WithOverrideWarning(t *testing.T) {
	setRequiredLoadEnv(t)
	output := captureSlogOutput(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "verify-ca")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "true")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Postgres.SSLMode != "verify-ca" {
		t.Fatalf("Postgres.SSLMode = %q, want verify-ca", config.Postgres.SSLMode)
	}

	warning := output.String()
	for _, want := range []string{"POSTGRES_SSLMODE=verify-ca", "POSTGRES_SSLMODE_ALLOW_INSECURE=true"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning output = %q, want %q", warning, want)
		}
	}
}

func TestLoad_ProductionAllowsVerifyFullPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "verify-full")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Postgres.SSLMode != "verify-full" {
		t.Fatalf("Postgres.SSLMode = %q, want verify-full", config.Postgres.SSLMode)
	}
}

func TestLoad_ProductionAllowsWeakPostgresSSLMode_WithOverrideWarning(t *testing.T) {
	setRequiredLoadEnv(t)
	output := captureSlogOutput(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("POSTGRES_SSLMODE_ALLOW_INSECURE", "true")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	warning := output.String()
	for _, want := range []string{"POSTGRES_SSLMODE=require", "POSTGRES_SSLMODE_ALLOW_INSECURE=true"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning output = %q, want %q", warning, want)
		}
	}
}

func TestLoad_DevelopmentAllowsWeakPostgresSSLMode(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("POSTGRES_SSLMODE", "prefer")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Postgres.SSLMode != "prefer" {
		t.Fatalf("Postgres.SSLMode = %q, want prefer", config.Postgres.SSLMode)
	}
}

func TestLoadAdminAPI_ProductionRejectsInsecurePostgresSSLMode(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("APP_ENV", "production")
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := LoadAdminAPI()
	if err == nil {
		t.Fatal("LoadAdminAPI() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
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
	t.Setenv("POSTGRES_SSLMODE", "require")

	_, err := LoadLLMScheduler()
	if err == nil {
		t.Fatal("LoadLLMScheduler() expected production sslmode validation error, got nil")
	}
	if !strings.Contains(err.Error(), "POSTGRES_SSLMODE=require is not allowed in production") {
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

	config, err := Load()
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
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNews.Confidence != 0.0 {
			t.Errorf("ConsensusConfidence = %v, want 0.0", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("above 1 clamped to 1", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "1.5")
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNews.Confidence != 1.0 {
			t.Errorf("ConsensusConfidence = %v, want 1.0", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("NaN falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "NaN")
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", config.LLM.MemberNews.Confidence)
		}
	})

	t.Run("Inf falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "Inf")
		config, err := Load()
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
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNews.ReviewTimeout != 30 {
			t.Errorf("ConsensusReviewTimeout = %d, want 30 (default on <5)", config.LLM.MemberNews.ReviewTimeout)
		}
	})

	t.Run("adjudicate timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_ADJUDICATE_TIMEOUT_SEC", "3")
		config, err := Load()
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
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		// config 레벨에서는 빈값 유지, provider 레벨에서 fallback
		if config.LLM.MemberNews.ReviewerModel != "" {
			t.Errorf("ConsensusReviewerModel = %q, want empty (fallback at provider level)", config.LLM.MemberNews.ReviewerModel)
		}
	})

	t.Run("explicit reviewer model preserved", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEWER_MODEL", "gpt-4.1-mini")
		config, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if config.LLM.MemberNews.ReviewerModel != "gpt-4.1-mini" {
			t.Errorf("ConsensusReviewerModel = %q, want gpt-4.1-mini", config.LLM.MemberNews.ReviewerModel)
		}
	})
}

func TestLoadAdminAPI_EnvApplied(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("ADMIN_API_PORT", "39002")
	t.Setenv("LOG_LEVEL", "")

	config, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if config.Server.Port != 39002 {
		t.Fatalf("Server.Port = %d, want %d", config.Server.Port, 39002)
	}
	if config.Logging.Level != "info" {
		t.Fatalf("Logging.Level = %q, want %q", config.Logging.Level, "info")
	}
}

func TestLoadAdminAPI_CORSLooseBoolParsing(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("CORS_ENFORCE", "yes")

	config, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if !config.CORS.Enforce {
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

	config, err := Load()
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

	config, err := Load()
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

func TestLoad_WebhookUsesIrisBotWorkerProfile(t *testing.T) {
	setRequiredLoadEnv(t)
	server := newIrisRuntimeDiagnosticsServer(t, `{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": true,
					"workerProfile": {
						"version": 1,
						"profile_id": "hololive-custom-test",
						"delivery": {
							"lane_workers": 32,
							"lane_queue_capacity": 128,
							"max_global_in_flight": 32,
							"max_per_endpoint_in_flight": 8,
							"max_drain_per_tick": 128,
							"max_attempts": 6,
							"request_timeout_ms": 30000,
							"lane_idle_timeout_ms": 750
						},
						"receive": {
							"workers": 20,
							"queue_size": 640,
							"enqueue_timeout_ms": 80,
							"handler_timeout_ms": 36000,
							"max_body_bytes": 262144,
							"dedup_ttl_ms": 120000,
							"dedup_timeout_ms": 300
						},
						"bot_pool": {
							"workers": 15,
							"queue_size": 200
						},
						"validation": {
							"min_queue_per_endpoint_multiplier": 4,
							"require_receive_capacity_for_endpoint_burst": true
						}
					}
				}
			}
		}
	}`)
	t.Setenv("IRIS_BASE_URL", server.URL)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Webhook.WorkerCount != 20 {
		t.Fatalf("Webhook.WorkerCount = %d, want 20", config.Webhook.WorkerCount)
	}
	if config.Webhook.QueueSize != 640 {
		t.Fatalf("Webhook.QueueSize = %d, want 640", config.Webhook.QueueSize)
	}
	if config.Webhook.EnqueueTimeout != 80*time.Millisecond {
		t.Fatalf("Webhook.EnqueueTimeout = %v, want 80ms", config.Webhook.EnqueueTimeout)
	}
	if config.Webhook.HandlerTimeout != 36*time.Second {
		t.Fatalf("Webhook.HandlerTimeout = %v, want 36s", config.Webhook.HandlerTimeout)
	}
	if config.Webhook.MaxBodyBytes != 262144 {
		t.Fatalf("Webhook.MaxBodyBytes = %d, want 262144", config.Webhook.MaxBodyBytes)
	}
	if config.Webhook.DedupTTL != 2*time.Minute || config.Webhook.DedupTimeout != 300*time.Millisecond {
		t.Fatalf("Webhook dedup = (%v,%v), want (2m,300ms)", config.Webhook.DedupTTL, config.Webhook.DedupTimeout)
	}
	if config.WorkerPool.Workers != 15 || config.WorkerPool.QueueSize != 200 {
		t.Fatalf("WorkerPool = (%d,%d), want (15,200)", config.WorkerPool.Workers, config.WorkerPool.QueueSize)
	}
	if config.WorkerProfile.Version != workerconfig.CurrentVersion {
		t.Fatalf("WorkerProfile.Version = %d, want %d", config.WorkerProfile.Version, workerconfig.CurrentVersion)
	}
	if config.WorkerProfile.Hash == "" {
		t.Fatal("WorkerProfile.Hash is empty")
	}
}

func TestLoad_BackwardCompatibleLLMServiceHealthURL(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVICES_LLM_SERVER_HEALTH_URL", "http://legacy-llm-server/health")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Services.LLMSchedulerHealthURL != "http://legacy-llm-server/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want legacy value", config.Services.LLMSchedulerHealthURL)
	}
}

func TestLoadAdminAPI_BackwardCompatibleLLMServiceHealthURL(t *testing.T) {
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("SERVICES_LLM_SERVER_HEALTH_URL", "http://legacy-llm-server/health")

	config, err := LoadAdminAPI()
	if err != nil {
		t.Fatalf("LoadAdminAPI() error = %v", err)
	}
	if config.Services.LLMSchedulerHealthURL != "http://legacy-llm-server/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want legacy value", config.Services.LLMSchedulerHealthURL)
	}
}

func TestLoad_WebhookRequireHTTP2(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("WEBHOOK_REQUIRE_HTTP2", "true")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !config.Webhook.RequireHTTP2 {
		t.Fatal("Webhook.RequireHTTP2 = false, want true")
	}
}

func TestLoad_ScraperSchedulerDefaults(t *testing.T) {
	setRequiredLoadEnv(t)

	config, err := Load()
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

	config, err := Load()
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

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS") {
		t.Fatalf("Load() error = %v", err)
	}
}

func clearYouTubeProducerGlobalBudgetEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED",
		"YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS",
		"YOUTUBE_PRODUCER_ACTIVE_ACTIVE_INSTANCE_COUNT",
		"YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT",
		"YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT",
		"YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT",
		"YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT",
		"YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT",
		"YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q) error = %v", key, err)
		}
	}
}

func assertYouTubeProducerGlobalBudgetConfig(t *testing.T, got, want YouTubeProducerGlobalBudgetConfig) {
	t.Helper()
	if got != want {
		t.Fatalf("LoadYouTubeProducerGlobalBudgetConfig() = %+v, want %+v", got, want)
	}
}

func TestLoadYouTubeProducerGlobalBudgetConfigDefaults(t *testing.T) {
	clearYouTubeProducerGlobalBudgetEnv(t)

	config := LoadYouTubeProducerGlobalBudgetConfig()

	assertYouTubeProducerGlobalBudgetConfig(t, config, YouTubeProducerGlobalBudgetConfig{
		Enabled:                    false,
		AcquireTimeout:             3 * time.Second,
		ActiveInstanceCount:        0,
		YouTubeScraperMaxInflight:  6,
		HolodexLiveMaxInflight:     4,
		BrowserSnapshotMaxInflight: 1,
		BackfillMaxInflight:        2,
		FallbackMaxInflight:        2,
		WindowCheckEnabled:         false,
	})
}

func TestLoadYouTubeProducerGlobalBudgetConfigEnvOverrides(t *testing.T) {
	clearYouTubeProducerGlobalBudgetEnv(t)
	t.Setenv("YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED", "true")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS", "2500")
	t.Setenv("YOUTUBE_PRODUCER_ACTIVE_ACTIVE_INSTANCE_COUNT", "3")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT", "7")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT", "5")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT", "2")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT", "4")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT", "8")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED", "true")

	config := LoadYouTubeProducerGlobalBudgetConfig()

	assertYouTubeProducerGlobalBudgetConfig(t, config, YouTubeProducerGlobalBudgetConfig{
		Enabled:                    true,
		AcquireTimeout:             2500 * time.Millisecond,
		ActiveInstanceCount:        3,
		YouTubeScraperMaxInflight:  7,
		HolodexLiveMaxInflight:     5,
		BrowserSnapshotMaxInflight: 2,
		BackfillMaxInflight:        4,
		FallbackMaxInflight:        8,
		WindowCheckEnabled:         true,
	})
}

func TestLoadYouTubeProducerGlobalBudgetConfigAcquireTimeoutClamp(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{name: "zero", env: "0", want: 3 * time.Second},
		{name: "negative", env: "-1", want: 3 * time.Second},
		{name: "above max", env: "6000", want: 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearYouTubeProducerGlobalBudgetEnv(t)
			t.Setenv("YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS", tt.env)

			config := LoadYouTubeProducerGlobalBudgetConfig()

			if config.AcquireTimeout != tt.want {
				t.Fatalf("AcquireTimeout = %s, want %s", config.AcquireTimeout, tt.want)
			}
		})
	}
}

func TestLoadYouTubeProducerGlobalBudgetConfigNegativeMaxInflight(t *testing.T) {
	clearYouTubeProducerGlobalBudgetEnv(t)
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT", "-1")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT", "-2")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT", "-3")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT", "-4")
	t.Setenv("YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT", "-5")

	config := LoadYouTubeProducerGlobalBudgetConfig()

	if config.YouTubeScraperMaxInflight != 0 {
		t.Fatalf("YouTubeScraperMaxInflight = %d, want 0", config.YouTubeScraperMaxInflight)
	}
	if config.HolodexLiveMaxInflight != 0 {
		t.Fatalf("HolodexLiveMaxInflight = %d, want 0", config.HolodexLiveMaxInflight)
	}
	if config.BrowserSnapshotMaxInflight != 0 {
		t.Fatalf("BrowserSnapshotMaxInflight = %d, want 0", config.BrowserSnapshotMaxInflight)
	}
	if config.BackfillMaxInflight != 0 {
		t.Fatalf("BackfillMaxInflight = %d, want 0", config.BackfillMaxInflight)
	}
	if config.FallbackMaxInflight != 0 {
		t.Fatalf("FallbackMaxInflight = %d, want 0", config.FallbackMaxInflight)
	}
}
