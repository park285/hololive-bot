package readiness

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestStateResponseActiveActiveStartsLeaseUnavailable(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		ActiveActiveEnabled:  true,
		ActiveActiveInstance: "youtube-producer-a",
	})
	state.MarkRunning()

	statusCode, payload := state.Response()

	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["valkey_available"] != false {
		t.Fatalf("valkey_available = %v, want false", payload["valkey_available"])
	}
	if payload["scraping_paused"] != true {
		t.Fatalf("scraping_paused = %v, want true", payload["scraping_paused"])
	}
	if payload["reason"] != "valkey_unavailable_active_active_fail_closed" {
		t.Fatalf("reason = %v, want valkey_unavailable_active_active_fail_closed", payload["reason"])
	}
}

func TestStateResponseSingleOwnerStartsLeaseAvailable(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: false,
	})
	state.MarkRunning()

	statusCode, payload := state.Response()

	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["valkey_available"] != true {
		t.Fatalf("valkey_available = %v, want true", payload["valkey_available"])
	}
	if payload["scraping_paused"] != false {
		t.Fatalf("scraping_paused = %v, want false", payload["scraping_paused"])
	}
}

func TestStateResponseActiveActiveLeaseUnavailableIsNotReady(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		ActiveActiveEnabled:  true,
		ActiveActiveInstance: "youtube-producer-a",
	})
	state.MarkRunning()
	state.MarkLeaseUnavailable("")

	statusCode, payload := state.Response()

	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
	if payload["job_lease_enabled"] != true {
		t.Fatalf("job_lease_enabled = %v, want true", payload["job_lease_enabled"])
	}
	if payload["valkey_available"] != false {
		t.Fatalf("valkey_available = %v, want false", payload["valkey_available"])
	}
	if payload["scraping_paused"] != true {
		t.Fatalf("scraping_paused = %v, want true", payload["scraping_paused"])
	}
	if payload["reason"] != "valkey_unavailable_active_active_fail_closed" {
		t.Fatalf("reason = %v, want valkey_unavailable_active_active_fail_closed", payload["reason"])
	}
}

func TestStateResponseLeaseAvailableRestoresReadiness(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: true,
	})
	state.MarkRunning()
	state.MarkLeaseUnavailable("custom_reason")
	state.MarkLeaseAvailable()

	statusCode, payload := state.Response()

	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	if payload["valkey_available"] != true {
		t.Fatalf("valkey_available = %v, want true", payload["valkey_available"])
	}
	if payload["scraping_paused"] != false {
		t.Fatalf("scraping_paused = %v, want false", payload["scraping_paused"])
	}
	if _, exists := payload["reason"]; exists {
		t.Fatal("reason should be omitted when lease is available")
	}
}

func TestStateResponseActiveActiveRecoversFromStartupFailClosed(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		ActiveActiveEnabled:  true,
		ActiveActiveInstance: "youtube-producer-a",
	})
	state.MarkRunning()

	state.MarkLeaseAvailable()
	statusCode, payload := state.Response()

	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	if payload["valkey_available"] != true {
		t.Fatalf("valkey_available = %v, want true", payload["valkey_available"])
	}
	if payload["scraping_paused"] != false {
		t.Fatalf("scraping_paused = %v, want false", payload["scraping_paused"])
	}
	if _, exists := payload["reason"]; exists {
		t.Fatal("reason should be omitted after active-active fail-closed recovery")
	}
	if state.LeaseAvailable() != true {
		t.Fatal("LeaseAvailable() = false, want true after recovery")
	}
}

func TestStateResponseBudgetBackendUnavailableIsNotReady(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	state.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")

	statusCode, payload := state.Response()

	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
	if payload["budget_backend_available"] != false {
		t.Fatalf("budget_backend_available = %v, want false", payload["budget_backend_available"])
	}
	if payload["scraping_paused"] != true {
		t.Fatalf("scraping_paused = %v, want true", payload["scraping_paused"])
	}
}

func TestStateResponseBudgetAdmissionDeniedDoesNotChangeHTTPReadiness(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	state.MarkBudgetAdmissionDenied("budget_exhausted", []string{"youtube_scraper", "holodex_live", "youtube_scraper"})
	state.MarkBudgetAdmissionDenied("source_cooldown", []string{"browser_snapshot", "holodex_live"})

	statusCode, payload := state.Response()

	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	if payload["budget_backend_available"] != true {
		t.Fatalf("budget_backend_available = %v, want true", payload["budget_backend_available"])
	}
	if payload["budget_exhausted"] != true {
		t.Fatalf("budget_exhausted = %v, want true", payload["budget_exhausted"])
	}
	if payload["source_cooldown"] != true {
		t.Fatalf("source_cooldown = %v, want true", payload["source_cooldown"])
	}
	wantSources := []string{"browser_snapshot", "holodex_live", "youtube_scraper"}
	if !reflect.DeepEqual(payload["affected_sources"], wantSources) {
		t.Fatalf("affected_sources = %v, want %v", payload["affected_sources"], wantSources)
	}
	if payload["scraping_paused"] != false {
		t.Fatalf("scraping_paused = %v, want false", payload["scraping_paused"])
	}
}

func TestStateResponseBudgetDisabledIgnoresBudgetState(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled: true,
	})
	state.MarkRunning()
	state.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")
	state.MarkBudgetAdmissionDenied("budget_exhausted", []string{"youtube_scraper"})

	statusCode, payload := state.Response()

	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["budget_backend_available"] != true {
		t.Fatalf("budget_backend_available = %v, want true", payload["budget_backend_available"])
	}
	if payload["budget_exhausted"] != false {
		t.Fatalf("budget_exhausted = %v, want false", payload["budget_exhausted"])
	}
	if payload["source_cooldown"] != false {
		t.Fatalf("source_cooldown = %v, want false", payload["source_cooldown"])
	}
	wantSources := []string{}
	if !reflect.DeepEqual(payload["affected_sources"], wantSources) {
		t.Fatalf("affected_sources = %v, want empty slice", payload["affected_sources"])
	}
	if payload["scraping_paused"] != false {
		t.Fatalf("scraping_paused = %v, want false", payload["scraping_paused"])
	}
}

func TestStateResponseSourceCooldownExpiresAfterTTL(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()

	base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	now := base
	state.nowFunc = func() time.Time { return now }

	state.MarkSourceCooldownFor([]string{"youtube_scraper"}, 30*time.Minute)

	_, payload := state.Response()
	if payload["source_cooldown"] != true {
		t.Fatalf("source_cooldown = %v, want true while TTL active", payload["source_cooldown"])
	}

	now = base.Add(31 * time.Minute)

	_, payload = state.Response()
	if payload["source_cooldown"] != false {
		t.Fatalf("source_cooldown = %v, want false after TTL expiry without any reserve", payload["source_cooldown"])
	}
	if affected, ok := payload["affected_sources"].([]string); !ok || len(affected) != 0 {
		t.Fatalf("affected_sources = %v, want empty after expiry", payload["affected_sources"])
	}
}
