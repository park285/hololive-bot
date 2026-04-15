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

package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
)

type ChannelStats struct {
	ChannelID       string
	ChannelTitle    string
	SubscriberCount uint64
	VideoCount      uint64
	ViewCount       uint64
	Timestamp       time.Time
}

type channelStatsAPIFallbackResult struct {
	stats             map[string]*ChannelStats
	successfulBatches int
}

type channelStatsScrapeResult struct {
	stats     map[string]*ChannelStats
	failedIDs []string
	scraped   int
}

// 스크래퍼를 우선 사용하고, 실패한 채널만 YouTube API로 폴백합니다.
// 이 방식으로 YouTube API quota를 절약합니다.
func (ys *serviceImpl) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*ChannelStats), nil
	}

	scrapeResult := ys.scrapeChannelStatistics(ctx, channelIDs)
	result, err := ys.completeChannelStatisticsAPIFallback(ctx, scrapeResult)
	if err != nil {
		return nil, err
	}
	ys.logger.Info("Channel statistics fetched (scraper+API)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(result)),
		slog.Int("scraped", scrapeResult.scraped),
		slog.Int("api_fallback", len(scrapeResult.failedIDs)))

	return result, nil
}

func (ys *serviceImpl) scrapeChannelStatistics(ctx context.Context, channelIDs []string) channelStatsScrapeResult {
	result := channelStatsScrapeResult{
		stats: make(map[string]*ChannelStats),
	}
	var mu sync.Mutex

	scraperCtx, scraperCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		constants.YouTubeConfig.ScraperPhaseTimeout,
	)
	defer scraperCancel()

	primary := fallback.RunPrimary(scraperCtx, channelIDs, fallback.FetchPlan[string, struct{}]{Parallelism: 5}, func(gctx context.Context, channelID string) error {
		stats, err := ys.scraper.GetChannelStats(gctx, channelID)
		if err != nil {
			ys.logger.Debug("Scraper failed, will fallback to API",
				slog.String("channelID", channelID),
				slog.Any("error", err))
			return fmt.Errorf("scraper channel stats for %s: %w", channelID, err)
		}

		mu.Lock()
		result.stats[channelID] = &ChannelStats{
			ChannelID:       stats.ChannelID,
			ChannelTitle:    ys.resolveChannelTitle(channelID, stats.Handle),
			SubscriberCount: uint64(stats.SubscriberCount),
			VideoCount:      uint64(stats.VideoCount),
			ViewCount:       uint64(stats.ViewCount),
			Timestamp:       time.Now(),
		}
		mu.Unlock()
		return nil
	})
	fallback.ObservePrimaryPhase("youtube", "channel_statistics", len(channelIDs), primary.Succeeded, len(primary.Failed))

	result.failedIDs = primary.Failed
	result.scraped = primary.Succeeded
	ys.logger.Info("Scraper phase completed",
		slog.Int("total", len(channelIDs)),
		slog.Int("scraped", result.scraped),
		slog.Int("failed", len(result.failedIDs)))
	return result
}

func (ys *serviceImpl) completeChannelStatisticsAPIFallback(ctx context.Context, scrapeResult channelStatsScrapeResult) (map[string]*ChannelStats, error) {
	result := scrapeResult.stats
	if len(scrapeResult.failedIDs) == 0 {
		_, _ = fallback.RunSecondary(ctx, fallback.SecondaryPlan{
			Service:   "youtube",
			Operation: "channel_statistics",
			Trigger:   fallback.TriggerOnFailures,
			ShouldRun: false,
		})
		return result, nil
	}

	apiCtx, apiCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		constants.YouTubeConfig.APIFallbackTimeout,
	)
	defer apiCancel()

	secondary, err := fallback.RunSecondary(apiCtx, fallback.SecondaryPlan{
		Service:   "youtube",
		Operation: "channel_statistics",
		Trigger:   fallback.TriggerOnFailures,
		ShouldRun: true,
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			apiResult, apiErr := ys.getChannelStatsFromAPI(runCtx, scrapeResult.failedIDs)
			if apiErr != nil {
				return fallback.SecondaryResult{}, apiErr
			}
			maps.Copy(result, apiResult.stats)
			return fallback.SecondaryResult{
				Items:     len(apiResult.stats),
				Successes: apiResult.successfulBatches,
			}, nil
		},
	})
	if err != nil {
		ys.logger.Warn("API fallback failed",
			slog.Int("channels", len(scrapeResult.failedIDs)),
			slog.Any("error", err))
		if shouldReturnFallbackError(len(result), len(scrapeResult.failedIDs), 0) {
			return nil, fmt.Errorf("get channel statistics: api fallback unavailable after scraper failures: %w", err)
		}
		return result, nil
	}
	if shouldReturnFallbackError(len(result), len(scrapeResult.failedIDs), secondary.Result.Successes) {
		return nil, fmt.Errorf("get channel statistics: scraper and api fallback failed for %d channels", len(scrapeResult.failedIDs))
	}
	return result, nil
}

func (ys *serviceImpl) resolveChannelTitle(channelID string, fallbackTitle string) string {
	channelTitle := ys.getChannelName(channelID)
	if channelTitle != "" {
		return channelTitle
	}
	return fallbackTitle
}

func (ys *serviceImpl) getChannelStatsFromAPI(ctx context.Context, channelIDs []string) (channelStatsAPIFallbackResult, error) {
	if len(channelIDs) == 0 {
		return channelStatsAPIFallbackResult{stats: make(map[string]*ChannelStats)}, nil
	}

	cost := len(channelIDs) * constants.YouTubeConfig.ChannelsQuotaCost
	if err := ys.checkQuota(cost); err != nil {
		return channelStatsAPIFallbackResult{}, err
	}

	batchSize := 50
	var batches [][]string
	for i := 0; i < len(channelIDs); i += batchSize {
		end := min(i+batchSize, len(channelIDs))
		batches = append(batches, channelIDs[i:end])
	}

	result := make(map[string]*ChannelStats)
	var mu sync.Mutex
	successfulBatches := 0
	var firstErr error
	var errMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for _, batch := range batches {
		g.Go(func() error {
			call := ys.service.Channels.List([]string{"statistics", "snippet"}).
				Id(batch...)

			response, err := call.Context(gctx).Do()
			if err != nil {
				ys.logger.Error("Failed to fetch channel statistics from API",
					slog.Int("batch_size", len(batch)),
					slog.Any("error", err))
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return nil
			}

			now := time.Now()
			mu.Lock()
			successfulBatches++
			for _, channel := range response.Items {
				result[channel.Id] = &ChannelStats{
					ChannelID:       channel.Id,
					ChannelTitle:    channel.Snippet.Title,
					SubscriberCount: channel.Statistics.SubscriberCount,
					VideoCount:      channel.Statistics.VideoCount,
					ViewCount:       channel.Statistics.ViewCount,
					Timestamp:       now,
				}
			}
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	if successfulBatches == 0 && firstErr != nil {
		return channelStatsAPIFallbackResult{}, fmt.Errorf("fetch channel statistics from API: %w", firstErr)
	}
	ys.consumeQuota(cost)

	ys.logger.Info("API fallback completed",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(result)),
		slog.Int("quota_used", cost))

	return channelStatsAPIFallbackResult{
		stats:             result,
		successfulBatches: successfulBatches,
	}, nil
}
