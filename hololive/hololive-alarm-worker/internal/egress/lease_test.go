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

func TestNotificationEgressLeaseAcquireReleaseRoundTrip(t *testing.T) {
	var acquiredOwner, releasedOwner string
	cache := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, _, value string, _ time.Duration) (bool, error) {
			acquiredOwner = value
			return true, nil
		},
		CompareAndDeleteFunc: func(_ context.Context, _, expected string) (bool, error) {
			releasedOwner = expected
			return true, nil
		},
	}

	lease, err := AcquireNotificationEgressLease(context.Background(), cache, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lease.Release(context.Background()); err != nil {
		t.Fatalf("release: %v", err)
	}
	if acquiredOwner == "" || acquiredOwner != releasedOwner {
		t.Fatalf("release owner %q != acquire owner %q", releasedOwner, acquiredOwner)
	}
}
