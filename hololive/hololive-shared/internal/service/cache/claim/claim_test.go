package claim

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCache_ClaimEmptyKeyReturnsErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  ClaimKey
	}{
		{name: "empty scope", key: ClaimKey{Subject: "video-1"}},
		{name: "empty subject", key: ClaimKey{Scope: "youtube_outbox_delivery"}},
		{name: "blank scope", key: ClaimKey{Scope: " ", Subject: "video-1"}},
		{name: "blank subject", key: ClaimKey{Scope: "youtube_outbox_delivery", Subject: "\t"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache := NewMemoryCache()
			_, err := cache.Claim(context.Background(), tt.key, "worker-a", time.Minute)
			if !errors.Is(err, ErrEmptyKey) {
				t.Fatalf("Claim() error = %v, want %v", err, ErrEmptyKey)
			}
		})
	}
}

func TestMemoryCache_ClaimEmptyHolderReturnsErr(t *testing.T) {
	t.Parallel()

	tests := []string{"", " ", "\n"}
	for _, holder := range tests {
		t.Run("holder "+holder, func(t *testing.T) {
			t.Parallel()

			cache := NewMemoryCache()
			_, err := cache.Claim(context.Background(), testClaimKey(), holder, time.Minute)
			if !errors.Is(err, ErrEmptyHolder) {
				t.Fatalf("Claim() error = %v, want %v", err, ErrEmptyHolder)
			}
		})
	}
}

func TestMemoryCache_ClaimZeroTTLReturnsErr(t *testing.T) {
	t.Parallel()

	tests := []time.Duration{0, -time.Second}
	for _, ttl := range tests {
		t.Run(ttl.String(), func(t *testing.T) {
			t.Parallel()

			cache := NewMemoryCache()
			_, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", ttl)
			if !errors.Is(err, ErrInvalidTTL) {
				t.Fatalf("Claim() error = %v, want %v", err, ErrInvalidTTL)
			}
		})
	}
}

func TestMemoryCache_ClaimNewKeyReturnsStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	cache := newMemoryCacheAt(now)

	status, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	if status.Holder != "worker-a" {
		t.Fatalf("Claim() holder = %q, want %q", status.Holder, "worker-a")
	}
	if !status.AcquiredAt.Equal(now) {
		t.Fatalf("Claim() acquired_at = %s, want %s", status.AcquiredAt, now)
	}
	if want := now.Add(30 * time.Second); !status.ExpiresAt.Equal(want) {
		t.Fatalf("Claim() expires_at = %s, want %s", status.ExpiresAt, want)
	}
	if status.RetryAfter != 0 {
		t.Fatalf("Claim() retry_after = %s, want 0", status.RetryAfter)
	}
}

func TestMemoryCache_ClaimExistingHolderReturnsAlreadyHeld(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	cache := newMemoryCacheAt(now)

	first, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("initial Claim() error = %v", err)
	}

	status, err := cache.Claim(context.Background(), testClaimKey(), "worker-b", time.Minute)
	if !errors.Is(err, ErrAlreadyHeld) {
		t.Fatalf("Claim() error = %v, want %v", err, ErrAlreadyHeld)
	}
	if status.Holder != "worker-a" {
		t.Fatalf("Claim() holder = %q, want original holder", status.Holder)
	}
	if !status.AcquiredAt.Equal(first.AcquiredAt) {
		t.Fatalf("Claim() acquired_at = %s, want %s", status.AcquiredAt, first.AcquiredAt)
	}
	if !status.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("Claim() expires_at = %s, want %s", status.ExpiresAt, first.ExpiresAt)
	}
	if status.RetryAfter != time.Minute {
		t.Fatalf("Claim() retry_after = %s, want %s", status.RetryAfter, time.Minute)
	}
}

func TestMemoryCache_ClaimAfterExpiryReturnsNewStatus(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	cache := NewMemoryCache()
	cache.now = func() time.Time { return current }

	if _, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", 10*time.Second); err != nil {
		t.Fatalf("initial Claim() error = %v", err)
	}

	current = current.Add(11 * time.Second)
	status, err := cache.Claim(context.Background(), testClaimKey(), "worker-b", 20*time.Second)
	if err != nil {
		t.Fatalf("Claim() after expiry error = %v", err)
	}
	if status.Holder != "worker-b" {
		t.Fatalf("Claim() holder = %q, want %q", status.Holder, "worker-b")
	}
	if !status.AcquiredAt.Equal(current) {
		t.Fatalf("Claim() acquired_at = %s, want %s", status.AcquiredAt, current)
	}
	if want := current.Add(20 * time.Second); !status.ExpiresAt.Equal(want) {
		t.Fatalf("Claim() expires_at = %s, want %s", status.ExpiresAt, want)
	}
}

func TestMemoryCache_ReleaseByOriginalHolderClears(t *testing.T) {
	t.Parallel()

	cache := newMemoryCacheAt(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	if _, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", time.Minute); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	if err := cache.Release(context.Background(), testClaimKey(), "worker-a"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	status, err := cache.Claim(context.Background(), testClaimKey(), "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("Claim() after Release() error = %v", err)
	}
	if status.Holder != "worker-b" {
		t.Fatalf("Claim() holder = %q, want %q", status.Holder, "worker-b")
	}
}

func TestMemoryCache_ReleaseByDifferentHolderReturnsMismatch(t *testing.T) {
	t.Parallel()

	cache := newMemoryCacheAt(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	if _, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", time.Minute); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	err := cache.Release(context.Background(), testClaimKey(), "worker-b")
	if !errors.Is(err, ErrHolderMismatch) {
		t.Fatalf("Release() error = %v, want %v", err, ErrHolderMismatch)
	}
}

func TestMemoryCache_ReleaseNonExistentIsIdempotent(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	if err := cache.Release(context.Background(), testClaimKey(), "worker-a"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestMemoryCache_ClaimRetryAfterIsExpiresMinusNow(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	cache := NewMemoryCache()
	cache.now = func() time.Time { return current }

	if _, err := cache.Claim(context.Background(), testClaimKey(), "worker-a", 30*time.Second); err != nil {
		t.Fatalf("initial Claim() error = %v", err)
	}

	current = current.Add(12 * time.Second)
	status, err := cache.Claim(context.Background(), testClaimKey(), "worker-b", time.Minute)
	if !errors.Is(err, ErrAlreadyHeld) {
		t.Fatalf("Claim() error = %v, want %v", err, ErrAlreadyHeld)
	}
	if want := 18 * time.Second; status.RetryAfter != want {
		t.Fatalf("Claim() retry_after = %s, want %s", status.RetryAfter, want)
	}
}

func newMemoryCacheAt(now time.Time) *MemoryCache {
	cache := NewMemoryCache()
	cache.now = func() time.Time { return now }
	return cache
}

func testClaimKey() ClaimKey {
	return ClaimKey{Scope: "youtube_outbox_delivery", Subject: "video-1"}
}
