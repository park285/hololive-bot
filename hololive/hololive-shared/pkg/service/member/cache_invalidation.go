package member

import (
	"context"
	"fmt"
	"log/slog"
)

func (c *Cache) InvalidateAll(ctx context.Context) error {
	if !c.epochCoordinated() {
		c.invalidateLocal()

		return nil
	}

	if err := c.invalidateCoordinated(ctx); err != nil {
		return fmt.Errorf("invalidate coordinated: %w", err)
	}

	return nil
}

func (c *Cache) invalidateLocal() {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	c.resetMemoryLocked()
}

func (c *Cache) invalidateCoordinated(ctx context.Context) error {
	epoch, err := c.advanceEpoch(ctx)
	if err != nil {
		return fmt.Errorf("advance epoch: %w", err)
	}

	c.publishEpochNotification(ctx, epoch)

	return nil
}

func (c *Cache) advanceEpoch(ctx context.Context) (uint64, error) {
	c.epochMu.Lock()
	defer c.epochMu.Unlock()

	epoch, err := c.epoch.Advance(ctx)
	if err != nil {
		c.markEpochUncertain(epochReconcileMutation, err)

		return 0, fmt.Errorf("advance member cache epoch: %w", err)
	}

	c.applyEpoch(epoch, epochReconcileMutation)

	return epoch, nil
}

func (c *Cache) publishEpochNotification(ctx context.Context, epoch uint64) {
	if err := c.epoch.Publish(ctx, epoch); err != nil {
		memberCacheEpochNotificationsTotal.WithLabelValues("failed").Inc()

		if c.logger != nil {
			c.logger.Warn("member cache epoch notification failed", slog.Uint64("epoch", epoch), slog.Any("error", err))
		}

		return
	}

	memberCacheEpochNotificationsTotal.WithLabelValues("sent").Inc()
}

func (c *Cache) Refresh(ctx context.Context) error {
	if err := c.InvalidateAll(ctx); err != nil {
		return fmt.Errorf("invalidate cache: %w", err)
	}

	if err := c.WarmUpCache(ctx); err != nil {
		return fmt.Errorf("warm up cache: %w", err)
	}

	return nil
}
