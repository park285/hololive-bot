package egress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/backoff"
)

const (
	NotificationEgressLeaseKey                 = "notification:egress-owner:alarm-worker"
	notificationEgressLeaseTTL                 = 30 * time.Second
	notificationEgressRenewGap                 = 10 * time.Second
	NotificationEgressLeaseAcquireRetrySeconds = 5

	notificationEgressRenewMaxAttempts = 3
	notificationEgressRenewBaseDelay   = 1 * time.Second
	notificationEgressRenewJitter      = 500 * time.Millisecond
)

var (
	ErrNotificationEgressLeaseHeld          = errors.New("notification egress lease held")
	errNotificationEgressLeaseOwnershipLost = errors.New("notification egress lease ownership lost")
)

type NotificationEgressLease struct {
	cacheClient cache.Client
	key         string
	owner       string
	ttl         time.Duration
	renewGap    time.Duration
	logger      *slog.Logger
	sleep       func(context.Context, time.Duration) bool
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
		sleep:       sleepWithContext,
	}, nil
}

func notificationEgressLeaseOwner() string {
	return util.InstanceID("alarm-worker")
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

	sleep := l.sleep
	if sleep == nil {
		sleep = sleepWithContext
	}

	var lastErr error
	for attempt := range notificationEgressRenewMaxAttempts {
		if attempt > 0 {
			delay := backoff.ComputeExponentialBackoff(attempt-1, notificationEgressRenewBaseDelay, 0, notificationEgressRenewJitter)
			if !sleep(ctx, delay) {
				return nil
			}
			l.logRenewRetry(attempt, lastErr, delay)
		}

		err := l.renewOnce(ctx)
		if err == nil {
			return nil
		}
		if errors.Is(err, errNotificationEgressLeaseOwnershipLost) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (l *NotificationEgressLease) renewOnce(ctx context.Context) error {
	renewed, err := l.cacheClient.CompareAndExpire(ctx, l.key, l.owner, l.ttl)
	if err != nil {
		return fmt.Errorf("renew notification egress lease: %w", err)
	}
	if !renewed {
		return fmt.Errorf("renew notification egress lease: %w: key=%s", errNotificationEgressLeaseOwnershipLost, l.key)
	}
	return nil
}

func (l *NotificationEgressLease) logRenewRetry(attempt int, err error, delay time.Duration) {
	if l.logger == nil {
		return
	}
	l.logger.Warn("Notification egress lease renew retrying",
		slog.String("event", "notification_egress_lease_renew_retry"),
		slog.String("key", l.key),
		slog.Int("attempt", attempt),
		slog.Duration("delay", delay),
		slog.Any("error", err),
	)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
