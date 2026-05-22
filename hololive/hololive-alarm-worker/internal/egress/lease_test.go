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
