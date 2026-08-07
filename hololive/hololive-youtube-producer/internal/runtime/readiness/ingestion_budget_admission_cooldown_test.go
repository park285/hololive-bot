package readiness

import (
	"testing"
	"time"
)

func newCooldownTestState(clock *time.Time) *State {
	state := New("youtube-producer", Features{YouTubeEnabled: true, GlobalBudgetEnabled: true})
	state.nowFunc = func() time.Time { return *clock }
	state.MarkRunning()
	return state
}

func sourceCooldownFlag(t *testing.T, state *State) bool {
	t.Helper()
	_, payload := state.Response()
	flag, ok := payload["source_cooldown"].(bool)
	if !ok {
		t.Fatalf("source_cooldown missing or not a bool: %v", payload["source_cooldown"])
	}
	return flag
}

func TestSourceCooldownDenialExpiresWithoutClearBudgetAdmission(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	state := newCooldownTestState(&now)

	state.MarkBudgetAdmissionDenied("source_cooldown", []string{"browser_snapshot"}, 30*time.Second)
	if !sourceCooldownFlag(t, state) {
		t.Fatal("denial must raise source_cooldown")
	}

	now = now.Add(31 * time.Second)
	if sourceCooldownFlag(t, state) {
		t.Fatal("source_cooldown must expire on its own; ClearBudgetAdmission never covers a fallback-only source")
	}
}

func TestSourceCooldownDenialWithoutRetryAfterDoesNotStickForever(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	state := newCooldownTestState(&now)

	state.MarkBudgetAdmissionDenied("source_cooldown", []string{"browser_snapshot"}, 0)

	now = now.Add(time.Second)
	if sourceCooldownFlag(t, state) {
		t.Fatal("a denial carrying no RetryAfter must not pin source_cooldown permanently")
	}
}

func TestSourceCooldownDenialDoesNotShortenAKnownLongerCooldown(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	state := newCooldownTestState(&now)

	state.MarkSourceCooldownFor([]string{"browser_snapshot"}, 10*time.Minute)
	state.MarkBudgetAdmissionDenied("source_cooldown", []string{"browser_snapshot"}, time.Second)

	now = now.Add(2 * time.Second)
	if !sourceCooldownFlag(t, state) {
		t.Fatal("a shorter denial-derived expiry must not shorten the longer recorded cooldown")
	}
}

func TestSourceCooldownDenialExtendsAKnownShorterCooldown(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	state := newCooldownTestState(&now)

	state.MarkSourceCooldownFor([]string{"browser_snapshot"}, time.Second)
	state.MarkBudgetAdmissionDenied("source_cooldown", []string{"browser_snapshot"}, 10*time.Minute)

	now = now.Add(2 * time.Second)
	if !sourceCooldownFlag(t, state) {
		t.Fatal("a longer denial-derived expiry must extend the recorded cooldown")
	}
}
