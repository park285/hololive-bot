// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package member

import (
	"context"
	"fmt"
	"log/slog"
)

func (c *Cache) InvalidateAll(ctx context.Context) error {
	if c.epoch != nil {
		return c.invalidateCoordinated(ctx)
	}
	return c.invalidateLegacy(ctx)
}

func (c *Cache) invalidateLegacy(ctx context.Context) error {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	c.snapshotGeneration.Add(1)
	c.byChannelID.Clear()
	c.byName.Clear()
	c.allMembers.Clear()
	c.allMembersSnapshot.Store(nil)

	if !c.cacheEnabled() {
		c.logger.Info("Member cache invalidated", slog.Int("keys_deleted", 0))
		return nil
	}

	keys, err := c.cache.ScanKeys(ctx, memberCachePattern, 100)
	if err != nil {
		return fmt.Errorf("failed to scan keys for invalidation: %w", err)
	}
	if len(keys) > 0 {
		if _, err := c.cache.DelMany(ctx, keys); err != nil {
			return fmt.Errorf("failed to invalidate cache store: %w", err)
		}
	}

	c.logger.Info("Member cache invalidated", slog.Int("keys_deleted", len(keys)))
	return nil
}

func (c *Cache) invalidateCoordinated(ctx context.Context) error {
	epoch, err := c.advanceEpoch(ctx)
	if err != nil {
		return err
	}
	c.publishEpochNotification(ctx, epoch)
	c.deleteLegacyMemberKeys(ctx)
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
	if err := c.acceptEpoch(epoch, epochReconcileMutation); err != nil {
		return 0, err
	}
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

func (c *Cache) deleteLegacyMemberKeys(ctx context.Context) {
	if !c.cacheEnabled() {
		return
	}
	keys, err := c.cache.ScanKeys(ctx, memberCachePattern, 100)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("legacy member cache scan failed after epoch advance", slog.Any("error", err))
		}
		return
	}
	if len(keys) == 0 {
		return
	}
	if _, err := c.cache.DelMany(ctx, keys); err != nil && c.logger != nil {
		c.logger.Warn("legacy member cache delete failed after epoch advance", slog.Any("error", err))
	}
}

func (c *Cache) Refresh(ctx context.Context) error {
	if err := c.InvalidateAll(ctx); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}
	return c.WarmUpCache(ctx)
}

func (c *Cache) InvalidateAliasCache(ctx context.Context, alias string) error {
	if c.epoch != nil {
		return c.InvalidateAll(ctx)
	}
	if !c.cacheEnabled() {
		c.logger.Info("Alias cache invalidated", slog.String("alias", alias))
		return nil
	}

	aliasKey := memberAliasKeyPrefix + alias
	if err := c.cache.Del(ctx, aliasKey); err != nil {
		c.logger.Warn("Failed to invalidate alias cache", slog.String("alias", alias), slog.Any("error", err))
		return fmt.Errorf("failed to invalidate alias cache: %w", err)
	}

	c.logger.Info("Alias cache invalidated", slog.String("alias", alias))
	return nil
}
