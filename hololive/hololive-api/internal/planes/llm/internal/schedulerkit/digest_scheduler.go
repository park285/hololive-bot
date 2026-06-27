package schedulerkit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type DigestScheduler struct {
	Locker  delivery.NotificationLocker
	Logger  *slog.Logger
	runtime *Runtime
}

func NewDigestScheduler(locker delivery.NotificationLocker, logger *slog.Logger) *DigestScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}
	return &DigestScheduler{
		Locker:  locker,
		Logger:  logger,
		runtime: NewRuntime(),
	}
}

func (d *DigestScheduler) SetClock(clockFn func() time.Time) {
	if d == nil {
		return
	}
	d.runtime.SetClock(clockFn)
}

func (d *DigestScheduler) Clock() time.Time {
	if d == nil || d.runtime == nil {
		return time.Now()
	}
	return d.runtime.Now()
}

func (d *DigestScheduler) Start(ctx context.Context, cfg *Config) {
	if d == nil {
		return
	}
	d.runtime.Start(ctx, cfg)
}

func (d *DigestScheduler) Stop() {
	if d == nil {
		return
	}
	d.runtime.Stop()
}

type DigestOp[C any] struct {
	LockKey           string
	OnLockNotAcquired func() error
	Collect           func(ctx context.Context) (C, bool, error)
	Execute           func(ctx context.Context, collected C) error
}

func RunDigest[C any](ctx context.Context, d *DigestScheduler, op DigestOp[C]) error {
	token, acquired, err := d.Locker.TryAcquire(ctx, op.LockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return handleDigestLockNotAcquired(op.OnLockNotAcquired)
	}
	defer d.releaseDigestLock(ctx, op.LockKey, token)

	collected, ok, err := op.Collect(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return op.Execute(ctx, collected)
}

func handleDigestLockNotAcquired(onLockNotAcquired func() error) error {
	if onLockNotAcquired != nil {
		return onLockNotAcquired()
	}
	return nil
}

func (d *DigestScheduler) releaseDigestLock(ctx context.Context, lockKey, token string) {
	if releaseErr := d.Locker.Release(ctx, lockKey, token); releaseErr != nil && d.Logger != nil {
		d.Logger.Warn("release digest lock failed", slog.String("lock_key", lockKey), slog.Any("error", releaseErr))
	}
}

func ShouldMark(result delivery.SendResult) (bool, error) {
	switch {
	case result.Sent == 0 && result.Failed > 0:
		return false, fmt.Errorf("all %d room(s) failed to enqueue", result.Failed)
	case result.Failed > 0:
		return false, nil
	default:
		return true, nil
	}
}
