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

package httpserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

func (h *StreamHandler) GetChannelStats(c *gin.Context) {
	ctx := c.Request.Context()

	handled, err := h.respondChannelStatsFromCache(c, ctx)
	if err != nil || handled {
		return
	}

	handled, err = h.respondChannelStatsFromDB(c, ctx)
	if err != nil || handled {
		return
	}

	h.respondError(c, 503, "Channel stats snapshot not ready", gin.H{
		"code": "channel_stats_snapshot_not_ready",
		"hint": "retry later after background poller sync",
	})
}

func (h *StreamHandler) respondChannelStatsFromCache(c *gin.Context, ctx context.Context) (bool, error) {
	if h.ValkeyCache == nil {
		return false, nil
	}

	var cachedStats map[string]*youtube.ChannelStats
	if err := h.ValkeyCache.Get(ctx, ChannelStatsCacheKey, &cachedStats); err != nil {
		h.respondInternalError(
			c,
			"Failed to get channel stats",
			"Failed to get channel stats from cache",
			err,
		)
		return true, err
	}
	if cachedStats == nil {
		return false, nil
	}

	h.Logger.Debug("Channel stats cache hit", slog.Int("count", len(cachedStats)))
	c.JSON(200, gin.H{"status": "ok", "stats": cachedStats})
	return true, nil
}

func (h *StreamHandler) respondChannelStatsFromDB(c *gin.Context, ctx context.Context) (bool, error) {
	if h.StatsRepository == nil {
		return false, nil
	}

	stats, err := h.getChannelStatsFromDB(ctx)
	if err != nil {
		h.respondInternalError(
			c,
			"Failed to get channel stats",
			"Failed to get channel stats from DB",
			err,
		)
		return true, err
	}
	if len(stats) == 0 {
		return false, nil
	}

	h.Logger.Debug("Channel stats DB snapshot hit", slog.Int("count", len(stats)))
	h.cacheChannelStatsAsync(ctx, stats)
	h.triggerChannelStatsRefreshAsync(ctx)

	c.JSON(200, gin.H{"status": "ok", "stats": stats, "source": "db_snapshot"})
	return true, nil
}

func (h *StreamHandler) getChannelStatsFromDB(ctx context.Context) (map[string]*youtube.ChannelStats, error) {
	channelIDs, channelToName, err := h.GetActiveMemberIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("get members: %w", err)
	}

	if len(channelIDs) == 0 {
		return make(map[string]*youtube.ChannelStats), nil
	}

	dbStats, err := h.StatsRepository.GetLatestStatsForChannels(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("get latest stats: %w", err)
	}

	result := make(map[string]*youtube.ChannelStats, len(dbStats))
	for channelID, ts := range dbStats {
		title := ts.MemberName
		if title == "" {
			title = channelToName[channelID]
		}
		result[channelID] = &youtube.ChannelStats{
			ChannelID:       ts.ChannelID,
			ChannelTitle:    title,
			SubscriberCount: ts.SubscriberCount,
			VideoCount:      ts.VideoCount,
			ViewCount:       ts.ViewCount,
			Timestamp:       ts.Timestamp,
		}
	}

	return result, nil
}

func (h *StreamHandler) cacheChannelStatsAsync(ctx context.Context, stats map[string]*youtube.ChannelStats) {
	if h.ValkeyCache == nil || stats == nil {
		return
	}

	state := h.ensureState()
	h.runAsyncWithLimiter(state.channelStatsCacheLimiter, "cache_channel_stats", func() {
		cacheCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.CacheSaveTimeout,
		)
		defer cancel()

		if err := h.ValkeyCache.Set(cacheCtx, ChannelStatsCacheKey, stats, ChannelStatsCacheTTL); err != nil {
			h.Logger.Warn("Failed to cache channel stats", slog.Any("error", err))
		}
	})
}

func (h *StreamHandler) triggerChannelStatsRefreshAsync(ctx context.Context) {
	if h.ValkeyCache == nil || h.YouTube == nil {
		return
	}

	state := h.ensureState()
	h.runAsyncWithLimiter(state.channelStatsRefreshLimiter, "refresh_channel_stats", func() {
		bgCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.ScraperPhaseTimeout,
		)
		defer cancel()

		if !h.acquireChannelStatsRefreshLock(bgCtx) {
			return
		}

		h.refreshChannelStats(bgCtx)
	})
}

func (h *StreamHandler) acquireChannelStatsRefreshLock(ctx context.Context) bool {
	acquired, err := h.ValkeyCache.SetNX(ctx, ChannelStatsRefreshLockKey, ChannelStatsRefreshLockValue, ChannelStatsRefreshLockTTL)
	if err != nil {
		h.Logger.Warn("Failed to acquire refresh lock", slog.Any("error", err))
		return false
	}
	if !acquired {
		h.Logger.Debug("Refresh lock already held, skipping background refresh")
		return false
	}
	return true
}

func (h *StreamHandler) refreshChannelStats(ctx context.Context) {
	h.Logger.Info("Background channel stats refresh started")

	channelIDs, _, err := h.GetActiveMemberIndex(ctx)
	if err != nil {
		h.Logger.Warn("Background refresh: failed to get members", slog.Any("error", err))
		return
	}

	stats, err := h.YouTube.GetChannelStatistics(ctx, channelIDs)
	if err != nil {
		h.Logger.Warn("Background refresh: failed to get stats", slog.Any("error", err))
		return
	}

	h.cacheChannelStatsAsync(ctx, stats)
	h.Logger.Info("Background channel stats refresh completed", slog.Int("count", len(stats)))
}

func (h *StreamHandler) runAsyncWithLimiter(limiter chan struct{}, task string, fn func()) {
	if limiter == nil {
		go fn()
		return
	}

	select {
	case limiter <- struct{}{}:
		go func() {
			defer func() { <-limiter }()
			fn()
		}()
	default:
		h.Logger.Debug("Skip async task: limiter saturated", slog.String("task", task))
	}
}
