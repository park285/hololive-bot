package milestonescheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type recentVideosBatchStats struct {
	mu           sync.Mutex
	successCount int
	errorCount   int
}

func (ys *schedulerImpl) Start(ctx context.Context) {
	ys.ticker = time.NewTicker(schedulerInterval)

	ys.logger.Info("YouTube quota building scheduler started",
		slog.Duration("interval", schedulerInterval),
		slog.Int("channels_per_batch", channelsPerBatch),
		slog.Int("daily_quota_target", totalDailyQuota))

	go func() {
		ys.runSchedulerLoop(ctx)
	}()

	if ys.holodex != nil {
		ys.startMilestoneWatcher(ctx)
	}
}

func (ys *schedulerImpl) runSchedulerLoop(ctx context.Context) {
	for {
		if ys.handleNextSchedulerEvent(ctx) {
			return
		}
	}
}

func (ys *schedulerImpl) handleNextSchedulerEvent(ctx context.Context) bool {
	select {
	case <-ys.ticker.C:
		ys.runBatch(ctx)
		return false
	case <-ys.stopCh:
		ys.logger.Info("YouTube scheduler stopped")
		return true
	case <-ctx.Done():
		ys.logger.Info("YouTube scheduler context canceled")
		return true
	}
}

func (ys *schedulerImpl) startMilestoneWatcher(ctx context.Context) {
	ys.milestoneWatchTicker = time.NewTicker(milestoneWatchInterval)
	ys.logger.Info("Milestone watcher started",
		slog.Duration("interval", milestoneWatchInterval),
		slog.Float64("threshold_ratio", MilestoneThresholdRatio))

	go func() {
		ys.runMilestoneWatcherLoop(ctx)
	}()
}

func (ys *schedulerImpl) runMilestoneWatcherLoop(ctx context.Context) {
	ys.runMilestoneWatcherCycle(ctx)
	for {
		if ys.handleNextMilestoneWatcherEvent(ctx) {
			return
		}
	}
}

func (ys *schedulerImpl) handleNextMilestoneWatcherEvent(ctx context.Context) bool {
	select {
	case <-ys.milestoneWatchTicker.C:
		ys.runMilestoneWatcherCycle(ctx)
		return false
	case <-ys.stopCh:
		return true
	case <-ctx.Done():
		return true
	}
}

func (ys *schedulerImpl) runMilestoneWatcherCycle(ctx context.Context) {
	ys.watchNearMilestoneMembers(ctx)
	ys.dispatchMilestoneAlerts(ctx)
}

func (ys *schedulerImpl) Stop() {
	if ys.ticker != nil {
		ys.ticker.Stop()
	}
	if ys.milestoneWatchTicker != nil {
		ys.milestoneWatchTicker.Stop()
	}
	ys.stopOnce.Do(func() {
		close(ys.stopCh)
	})
}

func (ys *schedulerImpl) runBatch(ctx context.Context) {
	if !ys.batchRunning.CompareAndSwap(false, true) {
		ys.logger.Warn("Skipping YouTube quota building batch: previous batch still running")
		return
	}
	defer ys.batchRunning.Store(false)

	ys.batchMu.Lock()
	batchNum := ys.currentBatch
	ys.currentBatch = (ys.currentBatch + 1) % batchesPerDay
	ys.batchMu.Unlock()

	ys.logger.Info("Running YouTube quota building batch",
		slog.Int("batch", batchNum),
		slog.Int("total_batches", batchesPerDay))

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ys.trackAllSubscribers(ctx)
	}()

	go func() {
		defer wg.Done()
		ys.fetchRecentVideosRotation(ctx, batchNum)
	}()

	wg.Wait()
}

func (ys *schedulerImpl) fetchRecentVideosRotation(ctx context.Context, batchNum int) {
	channels := ys.getRotatingBatch(batchNum, channelsPerBatch)

	if len(channels) == 0 {
		ys.logger.Info("Skipping recent videos batch: no channels configured",
			slog.Int("batch", batchNum))
		return
	}

	ys.logger.Info("Fetching recent videos for batch",
		slog.Int("batch", batchNum),
		slog.Int("channels", len(channels)),
		slog.Int("quota_cost", len(channels)*100))

	stats := &recentVideosBatchStats{}
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(recentVideosFetchParallelism)

	for _, channelID := range channels {
		eg.Go(func() error {
			return ys.fetchAndCacheRecentVideos(egCtx, channelID, stats)
		})
	}
	if err := eg.Wait(); err != nil {
		ys.logger.Warn("Recent videos batch worker failed",
			slog.Int("batch", batchNum),
			slog.Any("error", err))
	}

	ys.logger.Info("Recent videos batch completed",
		slog.Int("batch", batchNum),
		slog.Int("success", stats.successCount),
		slog.Int("errors", stats.errorCount))
}

func (ys *schedulerImpl) fetchAndCacheRecentVideos(ctx context.Context, channelID string, stats *recentVideosBatchStats) error {
	videos, err := ys.youtube.GetRecentVideos(ctx, channelID, 10)
	if err != nil {
		stats.recordError()
		return nil
	}

	cacheKey := "youtube:recent_videos:" + channelID
	if cacheErr := ys.cache.Set(ctx, cacheKey, videos, 24*time.Hour); cacheErr != nil {
		stats.recordError()
		return nil
	}

	stats.recordSuccess()
	return nil
}

func (stats *recentVideosBatchStats) recordError() {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.errorCount++
}

func (stats *recentVideosBatchStats) recordSuccess() {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.successCount++
}

func (ys *schedulerImpl) getRotatingBatch(batchNum, size int) []string {
	allChannels := make([]string, 0, len(ys.membersData.GetAllMembers()))
	for _, member := range ys.membersData.GetAllMembers() {
		allChannels = append(allChannels, member.ChannelID)
	}

	total := len(allChannels)
	if total == 0 || size <= 0 {
		return []string{}
	}

	start := (batchNum * size) % total
	end := start + size

	if end <= total {
		return allChannels[start:end]
	}

	batch := make([]string, 0, size)
	batch = append(batch, allChannels[start:]...)
	batch = append(batch, allChannels[0:end-total]...)
	return batch
}
