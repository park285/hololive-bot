package youtube

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func (ys *schedulerImpl) Start(ctx context.Context) {
	ys.ticker = time.NewTicker(schedulerInterval)

	ys.logger.Info("YouTube quota building scheduler started",
		slog.Duration("interval", schedulerInterval),
		slog.Int("channels_per_batch", channelsPerBatch),
		slog.Int("daily_quota_target", totalDailyQuota))

	go func() {
		for {
			select {
			case <-ys.ticker.C:
				ys.runBatch(ctx)
			case <-ys.stopCh:
				ys.logger.Info("YouTube scheduler stopped")
				return
			case <-ctx.Done():
				ys.logger.Info("YouTube scheduler context canceled")
				return
			}
		}
	}()

	if ys.holodex != nil {
		ys.milestoneWatchTicker = time.NewTicker(milestoneWatchInterval)
		ys.logger.Info("Milestone watcher started",
			slog.Duration("interval", milestoneWatchInterval),
			slog.Float64("threshold_ratio", MilestoneThresholdRatio))

		go func() {
			ys.watchNearMilestoneMembers(ctx)
			ys.dispatchMilestoneAlerts(ctx)

			for {
				select {
				case <-ys.milestoneWatchTicker.C:
					ys.watchNearMilestoneMembers(ctx)
					ys.dispatchMilestoneAlerts(ctx)
				case <-ys.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}
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

	successCount := 0
	errorCount := 0
	var mu sync.Mutex
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(recentVideosFetchParallelism)

	for _, channelID := range channels {
		eg.Go(func() error {
			videos, err := ys.youtube.GetRecentVideos(egCtx, channelID, 10)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return nil
			}

			cacheKey := "youtube:recent_videos:" + channelID
			if cacheErr := ys.cache.Set(egCtx, cacheKey, videos, 24*time.Hour); cacheErr != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return nil
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()

	ys.logger.Info("Recent videos batch completed",
		slog.Int("batch", batchNum),
		slog.Int("success", successCount),
		slog.Int("errors", errorCount))
}

func (ys *schedulerImpl) getRotatingBatch(batchNum int, size int) []string {
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
