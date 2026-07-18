package readiness

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGoldenResponseSingleOwnerBudgetAdmissionDenied(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		GlobalBudgetEnabled:  true,
		ScraperFetcherEngine: "nethttp",
		ActiveActiveInstance: "youtube-producer-b",
	})
	state.MarkRunning()
	state.MarkBudgetAdmissionDenied("budget_exhausted", []string{"youtube_scraper"})

	code, payload := state.Response()

	if code != http.StatusOK {
		t.Fatalf("Response status = %d, want %d", code, http.StatusOK)
	}
	want := `{"active_active":false,"affected_sources":["youtube_scraper"],"budget_backend_available":true,"budget_cleanup_incomplete":false,"budget_exhausted":true,"goroutines":0,"http_server_started":true,"instance_id":"youtube-producer-b","job_lease_enabled":false,"mode":"single-owner","photo_sync_enabled":false,"runtime":"youtube-producer","scraper_fetcher_engine":"nethttp","scraping_paused":false,"shutting_down":false,"source_cooldown":false,"status":"ready","uptime":"UPTIME","valkey_available":true,"version":"VERSION","youtube_enabled":true}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("Response payload = %s, want %s", got, want)
	}
}

func TestGoldenResponseActiveActiveStartupFailClosed(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		ActiveActiveEnabled:  true,
		ActiveActiveInstance: "youtube-producer-a",
	})
	state.MarkRunning()

	code, payload := state.Response()

	if code != http.StatusServiceUnavailable {
		t.Fatalf("Response status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"active_active":true,"affected_sources":[],"budget_backend_available":true,"budget_cleanup_incomplete":false,"budget_exhausted":false,"goroutines":0,"http_server_started":true,"instance_id":"youtube-producer-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"reason":"valkey_unavailable_active_active_fail_closed","runtime":"youtube-producer","scraper_fetcher_engine":"nethttp","scraping_paused":true,"shutting_down":false,"source_cooldown":false,"status":"not_ready","uptime":"UPTIME","valkey_available":false,"version":"VERSION","youtube_enabled":true}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("Response payload = %s, want %s", got, want)
	}
}

func TestGoldenPublicResponseReady(t *testing.T) {
	state := New("youtube-producer", Features{YouTubeEnabled: true})
	state.MarkRunning()

	code, payload := state.PublicResponse()

	if code != http.StatusOK {
		t.Fatalf("PublicResponse status = %d, want %d", code, http.StatusOK)
	}
	want := `{"goroutines":0,"status":"ready","uptime":"UPTIME","version":"VERSION"}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("PublicResponse payload = %s, want %s", got, want)
	}
}

func TestGoldenPublicResponseActiveActiveNotReady(t *testing.T) {
	state := New("youtube-producer", Features{
		YouTubeEnabled:       true,
		ActiveActiveEnabled:  true,
		ActiveActiveInstance: "youtube-producer-a",
	})
	state.MarkRunning()

	code, payload := state.PublicResponse()

	if code != http.StatusServiceUnavailable {
		t.Fatalf("PublicResponse status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"goroutines":0,"status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("PublicResponse payload = %s, want %s", got, want)
	}
}

func TestGoldenNilStateResponse(t *testing.T) {
	var state *State

	code, payload := state.Response()

	if code != http.StatusServiceUnavailable {
		t.Fatalf("Response status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"affected_sources":[],"budget_backend_available":true,"budget_cleanup_incomplete":false,"budget_exhausted":false,"goroutines":0,"scraper_fetcher_engine":"nethttp","source_cooldown":false,"status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("Response payload = %s, want %s", got, want)
	}
}

func TestGoldenNilStatePublicResponse(t *testing.T) {
	var state *State

	code, payload := state.PublicResponse()

	if code != http.StatusServiceUnavailable {
		t.Fatalf("PublicResponse status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"goroutines":0,"status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if got := canonicalizeGolden(t, payload); got != want {
		t.Fatalf("PublicResponse payload = %s, want %s", got, want)
	}
}

func canonicalizeGolden(t *testing.T, payload map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("golden marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("golden unmarshal: %v, raw=%s", err, raw)
	}
	normalizeGoldenDynamicFields(t, decoded)
	out, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("golden re-marshal: %v", err)
	}
	return string(out)
}

func normalizeGoldenDynamicFields(t *testing.T, payload map[string]any) {
	t.Helper()

	for key, placeholder := range map[string]string{"version": "VERSION", "uptime": "UPTIME"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		if _, ok := value.(string); !ok {
			t.Fatalf("%s = %T, want string", key, value)
		}
		payload[key] = placeholder
	}
	if value, exists := payload["goroutines"]; exists {
		if _, ok := value.(float64); !ok {
			t.Fatalf("goroutines = %T, want number", value)
		}
		payload["goroutines"] = float64(0)
	}
}
