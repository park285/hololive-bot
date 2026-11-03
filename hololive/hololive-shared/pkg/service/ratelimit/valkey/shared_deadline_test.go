package valkey

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestSharedDeadlineExtendOnlyMovesLater(t *testing.T) {
	t.Parallel()

	deadline, mini, now := newTestSharedDeadline(t)

	if err := deadline.Extend(t.Context(), "holodex:api:cooldown", now.Add(10*time.Second)); err != nil {
		t.Fatalf("Extend(10s) error = %v", err)
	}

	if err := deadline.Extend(t.Context(), "holodex:api:cooldown", now.Add(5*time.Second)); err != nil {
		t.Fatalf("Extend(5s) error = %v", err)
	}

	assertSharedDeadline(t, deadline, "holodex:api:cooldown", now.Add(10*time.Second))

	if ttl := mini.TTL("holodex:api:cooldown"); ttl != 10*time.Second {
		t.Fatalf("TTL after earlier extend = %s, want 10s kept", ttl)
	}

	if err := deadline.Extend(t.Context(), "holodex:api:cooldown", now.Add(20*time.Second)); err != nil {
		t.Fatalf("Extend(20s) error = %v", err)
	}

	assertSharedDeadline(t, deadline, "holodex:api:cooldown", now.Add(20*time.Second))

	mini.FastForward(20 * time.Second)

	if mini.Exists("holodex:api:cooldown") {
		t.Fatal("cooldown key should expire at its deadline")
	}
}

func TestSharedDeadlineOpenOnceDoesNotExtendOpenWindow(t *testing.T) {
	t.Parallel()

	deadline, mini, now := newTestSharedDeadline(t)

	opened, err := deadline.OpenOnce(t.Context(), "holodex:api:circuit_open", now.Add(30*time.Second))
	if err != nil || !opened {
		t.Fatalf("OpenOnce(first) = %v, %v; want true", opened, err)
	}

	reopened, err := deadline.OpenOnce(t.Context(), "holodex:api:circuit_open", now.Add(60*time.Second))
	if err != nil || reopened {
		t.Fatalf("OpenOnce(second) = %v, %v; want false", reopened, err)
	}

	if ttl := mini.TTL("holodex:api:circuit_open"); ttl != 30*time.Second {
		t.Fatalf("TTL after second open = %s, want 30s kept", ttl)
	}

	assertSharedDeadline(t, deadline, "holodex:api:circuit_open", now.Add(30*time.Second))
}

func TestSharedDeadlineReadsMissingAsZeroAndRejectsCorruptValue(t *testing.T) {
	t.Parallel()

	deadline, mini, _ := newTestSharedDeadline(t)

	if got, err := deadline.Deadline(t.Context(), "holodex:api:cooldown"); err != nil || !got.IsZero() {
		t.Fatalf("Deadline(missing) = %v, %v; want zero", got, err)
	}

	if err := mini.Set("holodex:api:cooldown", "not-a-deadline"); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}

	if _, err := deadline.Deadline(t.Context(), "holodex:api:cooldown"); err == nil {
		t.Fatal("Deadline(corrupt) error = nil, want parse error")
	}
}

func newTestSharedDeadline(t *testing.T) (*SharedDeadline, *miniredis.Miniredis, time.Time) {
	t.Helper()

	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	deadline, err := NewSharedDeadline(client)
	if err != nil {
		t.Fatalf("NewSharedDeadline() error = %v", err)
	}

	now := time.UnixMilli(time.Now().UnixMilli())

	deadline.now = func() time.Time { return now }

	return deadline, mini, now
}

func assertSharedDeadline(t *testing.T, deadline *SharedDeadline, key string, want time.Time) {
	t.Helper()

	got, err := deadline.Deadline(t.Context(), key)
	if err != nil {
		t.Fatalf("Deadline(%q) error = %v", key, err)
	}

	if !got.Equal(want) {
		t.Fatalf("Deadline(%q) = %s, want %s", key, got, want)
	}
}
