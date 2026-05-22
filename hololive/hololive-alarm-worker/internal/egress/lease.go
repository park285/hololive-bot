package egress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	NotificationEgressLeaseKey                 = "notification:egress-owner:alarm-worker"
	notificationEgressLeaseTTL                 = 30 * time.Second
	notificationEgressRenewGap                 = 10 * time.Second
	NotificationEgressLeaseAcquireRetrySeconds = 5
)

var ErrNotificationEgressLeaseHeld = errors.New("notification egress lease held")

type NotificationEgressLease struct {
	cacheClient cache.Client
	key         string
	owner       string
	ttl         time.Duration
	renewGap    time.Duration
	logger      *slog.Logger
}

func AcquireNotificationEgressLease(ctx context.Context, cacheClient cache.Client, logger *slog.Logger) (*NotificationEgressLease, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("acquire notification egress lease: cache service must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	owner := notificationEgressLeaseOwner()
	acquired, err := cacheClient.SetNX(ctx, NotificationEgressLeaseKey, owner, notificationEgressLeaseTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire notification egress lease: setnx failed: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("acquire notification egress lease: %w: key=%s", ErrNotificationEgressLeaseHeld, NotificationEgressLeaseKey)
	}

	logger.Info("Notification egress lease acquired",
		slog.String("event", "notification_egress_lease_acquired"),
		slog.String("key", NotificationEgressLeaseKey),
		slog.String("owner", owner),
	)

	return &NotificationEgressLease{
		cacheClient: cacheClient,
		key:         NotificationEgressLeaseKey,
		owner:       owner,
		ttl:         notificationEgressLeaseTTL,
		renewGap:    notificationEgressRenewGap,
		logger:      logger,
	}, nil
}

func notificationEgressLeaseOwner() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("alarm-worker:%s:%d", hostname, os.Getpid())
}

func (l *NotificationEgressLease) RenewLoop(ctx context.Context) error {
	if l == nil {
		return nil
	}

	ticker := time.NewTicker(l.renewGap)
	defer ticker.Stop()

	for {
		done, err := l.waitAndRenew(ctx, ticker.C)
		if done {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (l *NotificationEgressLease) waitAndRenew(ctx context.Context, ticks <-chan time.Time) (bool, error) {
	select {
	case <-ctx.Done():
		return true, nil
	case <-ticks:
		return false, l.Renew(ctx)
	}
}

func (l *NotificationEgressLease) Renew(ctx context.Context) error {
	if l == nil {
		return nil
	}

	renewed, err := l.cacheClient.CompareAndExpire(ctx, l.key, l.owner, l.ttl)
	if err != nil {
		return fmt.Errorf("renew notification egress lease: %w", err)
	}
	if !renewed {
		return fmt.Errorf("renew notification egress lease: ownership lost: key=%s", l.key)
	}
	return nil
}

func (l *NotificationEgressLease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}

	released, err := l.cacheClient.CompareAndDelete(ctx, l.key, l.owner)
	if err != nil {
		return fmt.Errorf("release notification egress lease: %w", err)
	}
	if !released {
		return fmt.Errorf("release notification egress lease: ownership mismatch")
	}

	if l.logger != nil {
		l.logger.Info("Notification egress lease released",
			slog.String("event", "notification_egress_lease_released"),
			slog.String("key", l.key),
			slog.String("owner", l.owner),
		)
	}
	return nil
}
