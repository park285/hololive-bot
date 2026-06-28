package egress

import (
	"context"
	"errors"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestAcquireNotificationEgressLeaseSuccess(t *testing.T) {
	cache := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
			if key != NotificationEgressLeaseKey {
				t.Fatalf("key = %q, want %q", key, NotificationEgressLeaseKey)
			}
			if value == "" {
				t.Fatal("owner must not be empty")
			}
			if ttl != notificationEgressLeaseTTL {
				t.Fatalf("ttl = %v, want %v", ttl, notificationEgressLeaseTTL)
			}
			return true, nil
		},
	}

	lease, err := AcquireNotificationEgressLease(context.Background(), cache, nil)
	if err != nil {
		t.Fatalf("AcquireNotificationEgressLease() error = %v", err)
	}
	if lease == nil {
		t.Fatal("AcquireNotificationEgressLease() returned nil lease")
	}
}

func TestAcquireNotificationEgressLeaseHeld(t *testing.T) {
	cache := &cachemocks.Client{
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return false, nil
		},
	}

	_, err := AcquireNotificationEgressLease(context.Background(), cache, nil)
	if !errors.Is(err, ErrNotificationEgressLeaseHeld) {
		t.Fatalf("error = %v, want ErrNotificationEgressLeaseHeld", err)
	}
}

func TestNotificationEgressLeaseRenewAndReleaseUseOwner(t *testing.T) {
	var renewOwner string
	var releaseOwner string
	cache := &cachemocks.Client{
		CompareAndExpireFunc: func(_ context.Context, key, expected string, ttl time.Duration) (bool, error) {
			if key != NotificationEgressLeaseKey {
				t.Fatalf("renew key = %q, want %q", key, NotificationEgressLeaseKey)
			}
			if ttl != notificationEgressLeaseTTL {
				t.Fatalf("renew ttl = %v, want %v", ttl, notificationEgressLeaseTTL)
			}
			renewOwner = expected
			return true, nil
		},
		CompareAndDeleteFunc: func(_ context.Context, key, expected string) (bool, error) {
			if key != NotificationEgressLeaseKey {
				t.Fatalf("release key = %q, want %q", key, NotificationEgressLeaseKey)
			}
			releaseOwner = expected
			return true, nil
		},
	}
	lease := &NotificationEgressLease{
		cacheClient: cache,
		key:         NotificationEgressLeaseKey,
		owner:       "alarm-worker:test:1",
		ttl:         notificationEgressLeaseTTL,
		renewGap:    notificationEgressRenewGap,
	}

	if err := lease.Renew(context.Background()); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if err := lease.Release(context.Background()); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if renewOwner != lease.owner || releaseOwner != lease.owner {
		t.Fatalf("owners = renew %q release %q, want %q", renewOwner, releaseOwner, lease.owner)
	}
}

func newRenewTestLease(cache *cachemocks.Client, sleeps *int) *NotificationEgressLease {
	return &NotificationEgressLease{
		cacheClient: cache,
		key:         NotificationEgressLeaseKey,
		owner:       "alarm-worker:test:1",
		ttl:         notificationEgressLeaseTTL,
		renewGap:    notificationEgressRenewGap,
		sleep: func(context.Context, time.Duration) bool {
			(*sleeps)++
			return true
		},
	}
}

func TestNotificationEgressLeaseRenewRetriesTransientError(t *testing.T) {
	var calls, sleeps int
	cache := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			if calls < notificationEgressRenewMaxAttempts {
				return false, errors.New("valkey transient")
			}
			return true, nil
		},
	}

	if err := newRenewTestLease(cache, &sleeps).Renew(context.Background()); err != nil {
		t.Fatalf("Renew() error = %v, want nil after transient retries", err)
	}
	if calls != notificationEgressRenewMaxAttempts {
		t.Fatalf("CompareAndExpire calls = %d, want %d", calls, notificationEgressRenewMaxAttempts)
	}
	if sleeps != notificationEgressRenewMaxAttempts-1 {
		t.Fatalf("sleeps = %d, want %d", sleeps, notificationEgressRenewMaxAttempts-1)
	}
}

func TestNotificationEgressLeaseRenewOwnershipLostNoRetry(t *testing.T) {
	var calls, sleeps int
	cache := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			return false, nil
		},
	}

	err := newRenewTestLease(cache, &sleeps).Renew(context.Background())
	if !errors.Is(err, errNotificationEgressLeaseOwnershipLost) {
		t.Fatalf("error = %v, want errNotificationEgressLeaseOwnershipLost", err)
	}
	if calls != 1 {
		t.Fatalf("CompareAndExpire calls = %d, want 1 (ownership loss must not retry)", calls)
	}
	if sleeps != 0 {
		t.Fatalf("sleeps = %d, want 0", sleeps)
	}
}

func TestNotificationEgressLeaseRenewExhaustsTransientRetries(t *testing.T) {
	var calls, sleeps int
	cache := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			return false, errors.New("valkey down")
		},
	}

	err := newRenewTestLease(cache, &sleeps).Renew(context.Background())
	if err == nil {
		t.Fatal("Renew() = nil, want error after exhausting transient retries")
	}
	if errors.Is(err, errNotificationEgressLeaseOwnershipLost) {
		t.Fatalf("transient exhaustion must not be reported as ownership loss: %v", err)
	}
	if calls != notificationEgressRenewMaxAttempts {
		t.Fatalf("CompareAndExpire calls = %d, want %d", calls, notificationEgressRenewMaxAttempts)
	}
}

func TestNotificationEgressLeaseRenewGracefulOnContextCancelDuringBackoff(t *testing.T) {
	var calls int
	cache := &cachemocks.Client{
		CompareAndExpireFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			calls++
			return false, errors.New("transient")
		},
	}
	lease := &NotificationEgressLease{
		cacheClient: cache,
		key:         NotificationEgressLeaseKey,
		owner:       "alarm-worker:test:1",
		ttl:         notificationEgressLeaseTTL,
		renewGap:    notificationEgressRenewGap,
		sleep:       func(context.Context, time.Duration) bool { return false },
	}

	if err := lease.Renew(context.Background()); err != nil {
		t.Fatalf("Renew() error = %v, want nil when context cancels during backoff", err)
	}
	if calls != 1 {
		t.Fatalf("CompareAndExpire calls = %d, want 1 before backoff cancellation", calls)
	}
}
