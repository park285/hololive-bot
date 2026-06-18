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

package apiservice

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/service/fallback"
	ytcontract "github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ChannelStats = ytcontract.ChannelStats

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
		ytDefaults.ScraperPhaseTimeout,
	)
	defer scraperCancel()

	primary := fallback.RunPrimary(scraperCtx, channelIDs, fallback.FetchPlan[string, struct{}]{Parallelism: 5}, func(gctx context.Context, channelID string) error {
		stats, err := ys.scrapeSingleChannelStatistics(gctx, channelID)
		if err != nil {
			return err
		}
		mu.Lock()
		result.stats[channelID] = stats
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

func (ys *serviceImpl) scrapeSingleChannelStatistics(ctx context.Context, channelID string) (*ChannelStats, error) {
	stats, err := ys.scraper.GetChannelStats(ctx, channelID)
	if err != nil {
		ys.logger.Debug("Scraper failed, will fallback to API",
			slog.String("channelID", channelID),
			slog.Any("error", err))
		return nil, fmt.Errorf("scraper channel stats for %s: %w", channelID, err)
	}
	return ys.channelStatsFromScraped(channelID, stats)
}

func (ys *serviceImpl) channelStatsFromScraped(channelID string, stats *scraper.ChannelStats) (*ChannelStats, error) {
	subscriberCount, videoCount, viewCount, err := validatedScrapedChannelCounts(channelID, stats)
	if err != nil {
		return nil, err
	}
	return &ChannelStats{
		ChannelID:       stats.ChannelID,
		ChannelTitle:    ys.resolveChannelTitle(channelID, stats.Handle),
		SubscriberCount: subscriberCount,
		VideoCount:      videoCount,
		ViewCount:       viewCount,
		Timestamp:       time.Now(),
	}, nil
}

func validatedScrapedChannelCounts(channelID string, stats *scraper.ChannelStats) (subscriberCount, videoCount, viewCount uint64, err error) {
	subscriberCount, err = validatedScrapedChannelCount(channelID, "subscriber", stats.SubscriberCount)
	if err != nil {
		return 0, 0, 0, err
	}
	videoCount, err = validatedScrapedChannelCount(channelID, "video", stats.VideoCount)
	if err != nil {
		return 0, 0, 0, err
	}
	viewCount, err = validatedScrapedChannelCount(channelID, "view", stats.ViewCount)
	if err != nil {
		return 0, 0, 0, err
	}
	return subscriberCount, videoCount, viewCount, nil
}

func validatedScrapedChannelCount(channelID, label string, value int64) (uint64, error) {
	count, ok := nonNegativeYouTubeCount(value)
	if !ok {
		return 0, fmt.Errorf("scraper channel stats for %s: negative %s count %d", channelID, label, value)
	}
	return count, nil
}

func (ys *serviceImpl) completeChannelStatisticsAPIFallback(ctx context.Context, scrapeResult channelStatsScrapeResult) (map[string]*ChannelStats, error) {
	result := scrapeResult.stats
	if len(scrapeResult.failedIDs) == 0 {
		recordChannelStatisticsFallbackSkipped(ctx)
		return result, nil
	}

	secondary, err := ys.runChannelStatisticsAPIFallback(ctx, scrapeResult.failedIDs, result)
	if err != nil {
		return ys.handleChannelStatisticsFallbackError(scrapeResult, result, err)
	}
	if shouldReturnFallbackError(len(result), len(scrapeResult.failedIDs), secondary.Result.Successes) {
		return nil, fmt.Errorf("get channel statistics: scraper and api fallback failed for %d channels", len(scrapeResult.failedIDs))
	}
	return result, nil
}

func recordChannelStatisticsFallbackSkipped(ctx context.Context) {
	if _, err := fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "youtube",
		Operation: "channel_statistics",
		Trigger:   fallback.TriggerOnFailures,
		ShouldRun: false,
	}); err != nil {
		slog.Default().Warn("Failed to record channel statistics fallback skip", slog.Any("error", err))
	}
}

func (ys *serviceImpl) runChannelStatisticsAPIFallback(
	ctx context.Context,
	failedIDs []string,
	result map[string]*ChannelStats,
) (fallback.SecondaryExecution, error) {
	apiCtx, apiCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		ytDefaults.APIFallbackTimeout,
	)
	defer apiCancel()

	return fallback.RunSecondary(apiCtx, fallback.SecondaryPlan{
		Service:   "youtube",
		Operation: "channel_statistics",
		Trigger:   fallback.TriggerOnFailures,
		ShouldRun: true,
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			apiResult, apiErr := ys.getChannelStatsFromAPI(runCtx, failedIDs)
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
}

func (ys *serviceImpl) handleChannelStatisticsFallbackError(
	scrapeResult channelStatsScrapeResult,
	result map[string]*ChannelStats,
	err error,
) (map[string]*ChannelStats, error) {
	ys.logger.Warn("API fallback failed",
		slog.Int("channels", len(scrapeResult.failedIDs)),
		slog.Any("error", err))
	if shouldReturnFallbackError(len(result), len(scrapeResult.failedIDs), 0) {
		return nil, fmt.Errorf("get channel statistics: api fallback unavailable after scraper failures: %w", err)
	}
	return result, nil
}

func (ys *serviceImpl) resolveChannelTitle(channelID, fallbackTitle string) string {
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

	cost := len(channelIDs) * ytDefaults.ChannelsQuotaCost
	if err := ys.checkQuota(cost); err != nil {
		return channelStatsAPIFallbackResult{}, err
	}

	result, successfulBatches, firstErr := ys.fetchChannelStatsAPIBatches(ctx, channelIDs)
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

func (ys *serviceImpl) fetchChannelStatsAPIBatches(ctx context.Context, channelIDs []string) (result map[string]*ChannelStats, successfulBatches int, err error) {
	batches := channelStatsAPIBatches(channelIDs)
	result = make(map[string]*ChannelStats)
	collector := channelStatsAPIBatchCollector{
		result: &result,
	}
	g, gctx := errgroup.WithContext(ctx)

	for _, batch := range batches {
		g.Go(func() error {
			response, err := ys.fetchChannelStatsAPIBatch(gctx, batch)
			if err != nil {
				recordChannelStatsAPIError(ys.logger, batch, err)
				collector.recordError(err)
				return nil
			}

			collector.addSuccess(response, &successfulBatches)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return result, successfulBatches, err
	}
	return result, successfulBatches, collector.firstErr
}

type channelStatsAPIBatchCollector struct {
	result   *map[string]*ChannelStats
	firstErr error
	mu       sync.Mutex
	errMu    sync.Mutex
}

func (c *channelStatsAPIBatchCollector) recordError(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.firstErr == nil {
		c.firstErr = err
	}
}

func (c *channelStatsAPIBatchCollector) addSuccess(response *youtube.ChannelListResponse, successfulBatches *int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	(*successfulBatches)++
	addChannelStatsAPIResults(*c.result, response, time.Now())
}

func channelStatsAPIBatches(channelIDs []string) [][]string {
	const batchSize = 50
	batches := make([][]string, 0, (len(channelIDs)+batchSize-1)/batchSize)
	for i := 0; i < len(channelIDs); i += batchSize {
		end := min(i+batchSize, len(channelIDs))
		batches = append(batches, channelIDs[i:end])
	}
	return batches
}

func (ys *serviceImpl) fetchChannelStatsAPIBatch(ctx context.Context, batch []string) (*youtube.ChannelListResponse, error) {
	call := ys.service.Channels.List([]string{"statistics", "snippet"}).
		Id(batch...)
	return call.Context(ctx).Do()
}

func recordChannelStatsAPIError(logger *slog.Logger, batch []string, err error) {
	logger.Error("Failed to fetch channel statistics from API",
		slog.Int("batch_size", len(batch)),
		slog.Any("error", err))
}

func nonNegativeYouTubeCount(value int64) (uint64, bool) {
	if value < 0 {
		return 0, false
	}
	return uint64(value), true
}

func addChannelStatsAPIResults(result map[string]*ChannelStats, response *youtube.ChannelListResponse, now time.Time) {
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
}
