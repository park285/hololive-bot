package egress

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/lease"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	NotificationEgressLeaseKey                 = "notification:egress-owner:alarm-worker"
	notificationEgressLeaseTTL                 = 30 * time.Second
	notificationEgressRenewGap                 = 10 * time.Second
	NotificationEgressLeaseAcquireRetrySeconds = 5
)

var ErrNotificationEgressLeaseHeld = lease.ErrHeld

type NotificationEgressLease struct {
	inner  *lease.Lease
	logger *slog.Logger
}

func AcquireNotificationEgressLease(ctx context.Context, cacheClient cache.Client, logger *slog.Logger) (*NotificationEgressLease, error) {
	if logger == nil {
		logger = slog.Default()
	}

	owner := util.InstanceID("alarm-worker")
	inner, err := lease.Acquire(ctx, cacheClient, &lease.Spec{
		Name:     "notification-egress",
		Key:      NotificationEgressLeaseKey,
		Owner:    owner,
		TTL:      notificationEgressLeaseTTL,
		RenewGap: notificationEgressRenewGap,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("acquire notification egress lease: %w", err)
	}

	logger.Info("Notification egress lease acquired",
		slog.String("event", "notification_egress_lease_acquired"),
		slog.String("key", NotificationEgressLeaseKey),
		slog.String("owner", owner),
	)

	return &NotificationEgressLease{inner: inner, logger: logger}, nil
}

func (l *NotificationEgressLease) RenewLoop(ctx context.Context) error {
	if l == nil {
		return nil
	}
	return l.inner.RenewLoop(ctx)
}

func (l *NotificationEgressLease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if err := l.inner.Release(ctx); err != nil {
		return fmt.Errorf("release notification egress lease: %w", err)
	}

	if l.logger != nil {
		l.logger.Info("Notification egress lease released",
			slog.String("event", "notification_egress_lease_released"),
			slog.String("key", NotificationEgressLeaseKey),
			slog.String("owner", l.inner.Owner()),
		)
	}
	return nil
}
