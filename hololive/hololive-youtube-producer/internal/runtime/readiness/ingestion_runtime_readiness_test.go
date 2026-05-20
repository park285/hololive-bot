package readiness

import (
	"net/http"
	"testing"
)

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
	if _, exists := payload["reason"]; exists {
		t.Fatal("reason should be omitted when lease is available")
	}
}
