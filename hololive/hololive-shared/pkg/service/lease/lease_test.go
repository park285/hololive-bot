package lease

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newTestLease(c *cachemocks.Client, sleep func(context.Context, time.Duration) bool) *Lease {
	return &Lease{
		cache:       c,
		name:        "test",
		key:         "lock:test",
		owner:       "owner-1",
		ttl:         30 * time.Second,
		renewGap:    time.Millisecond,
		maxAttempts: 3,
		baseDelay:   time.Second,
		jitter:      500 * time.Millisecond,
		logger:      slog.Default(),
		sleep:       sleep,
	}
}

func countingSleep(n *int) func(context.Context, time.Duration) bool {
	return func(context.Context, time.Duration) bool {
		(*n)++
		return true
	}
}

func TestAcquireSetsOwnerAndTTL(t *testing.T) {
	spec := Spec{Name: "test", Key: "lock:test", Owner: "owner-1", TTL: 30 * time.Second, RenewGap: 10 * time.Second}
	c := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
			if key != spec.Key || value != spec.Owner || ttl != spec.TTL {
				t.Fatalf("SetNX(%q,%q,%v), want (%q,%q,%v)", key, value, ttl, spec.Key, spec.Owner, spec.TTL)
			}
			return true, nil
		},
	}

	l, err := Acquire(context.Background(), c, &spec, slog.Default())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if l == nil || l.Owner() != spec.Owner {
		t.Fatalf("Acquire() owner = %q, want %q", l.Owner(), spec.Owner)
	}
}

func TestNewLeaseDefaultsOmittedTuning(t *testing.T) {
	l := newLease(&cachemocks.Client{}, &Spec{Key: "k", Owner: "o", TTL: time.Second}, slog.Default())
	if l.maxAttempts != defaultRenewMaxAttempts || l.baseDelay != defaultRenewBaseDelay || l.jitter != defaultRenewJitter {
		t.Fatalf("defaults maxAttempts=%d baseDelay=%v jitter=%v, want %d/%v/%v",
			l.maxAttempts, l.baseDelay, l.jitter, defaultRenewMaxAttempts, defaultRenewBaseDelay, defaultRenewJitter)
	}
}

func TestAcquireHeld(t *testing.T) {
	c := &cachemocks.Client{
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) { return false, nil },
	}
	_, err := Acquire(context.Background(), c, &Spec{Key: "k", Owner: "o", TTL: time.Second}, slog.Default())
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("error = %v, want ErrHeld", err)
	}
}

func TestRenewRetriesTransientError(t *testing.T) {
	var calls, sleeps int
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(_ context.Context, key, expected string, _ time.Duration) (bool, error) {
			calls++
			if key != "lock:test" || expected != "owner-1" {
				t.Fatalf("CompareAndExpire(%q,%q), want (lock:test,owner-1)", key, expected)
			}
			if calls < 3 {
				return false, errors.New("valkey transient")
			}
			return true, nil
		},
	}
	if err := newTestLease(c, countingSleep(&sleeps)).Renew(context.Background()); err != nil {
		t.Fatalf("Renew() = %v, want nil after transient retries", err)
	}
	if calls != 3 || sleeps != 2 {
		t.Fatalf("calls=%d sleeps=%d, want 3/2", calls, sleeps)
	}
}

func TestRenewOwnershipLostNoRetry(t *testing.T) {
	var calls, sleeps int
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			return false, nil
		},
	}
	err := newTestLease(c, countingSleep(&sleeps)).Renew(context.Background())
	if !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("error = %v, want ErrOwnershipLost", err)
	}
	if calls != 1 || sleeps != 0 {
		t.Fatalf("calls=%d sleeps=%d, want 1/0 (no retry on ownership loss)", calls, sleeps)
	}
}

func TestRenewExhaustsTransientRetries(t *testing.T) {
	var calls, sleeps int
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			return false, errors.New("valkey down")
		},
	}
	err := newTestLease(c, countingSleep(&sleeps)).Renew(context.Background())
	if err == nil {
		t.Fatal("Renew() = nil, want error after exhausting retries")
	}
	if errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("transient exhaustion must not be ownership loss: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRenewGracefulOnContextCancelDuringBackoff(t *testing.T) {
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return false, errors.New("transient")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := newTestLease(c, func(context.Context, time.Duration) bool {
		cancel()
		return false
	})
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew() = %v, want nil when context cancels during backoff", err)
	}
}

func TestReleaseUsesOwnerAndDetectsMismatch(t *testing.T) {
	var released bool
	c := &cachemocks.Client{
		CompareAndDeleteFunc: func(_ context.Context, key, expected string) (bool, error) {
			if expected != "owner-1" {
				t.Fatalf("release owner = %q, want owner-1", expected)
			}
			return released, nil
		},
	}
	l := newTestLease(c, countingSleep(new(int)))

	released = false
	if err := l.Release(context.Background()); err == nil {
		t.Fatal("Release() = nil, want ownership mismatch error")
	}
	released = true
	if err := l.Release(context.Background()); err != nil {
		t.Fatalf("Release() = %v, want nil", err)
	}
}

func TestRenewLoopReturnsErrorOnOwnershipLost(t *testing.T) {
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return false, nil
		},
	}
	err := newTestLease(c, countingSleep(new(int))).RenewLoop(context.Background())
	if !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("RenewLoop() = %v, want ErrOwnershipLost", err)
	}
}

func TestRenewLoopRejectsNonPositiveRenewGap(t *testing.T) {
	l := newTestLease(&cachemocks.Client{}, countingSleep(new(int)))
	l.renewGap = 0
	if err := l.RenewLoop(context.Background()); err == nil {
		t.Fatal("RenewLoop() = nil, want error on non-positive renew gap")
	}
}

func TestRenewLoopStopsGracefullyOnContextDone(t *testing.T) {
	c := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return true, nil
		},
	}
	l := newTestLease(c, countingSleep(new(int)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.RenewLoop(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RenewLoop() = %v, want nil on context cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RenewLoop did not return after context cancel")
	}
}
