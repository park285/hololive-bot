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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/internal/service/fallback"
	ytcontract "github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ChannelStats = ytcontract.ChannelStats

type channelStatsScrapeResult struct {
	stats     map[string]*ChannelStats
	failedIDs []string
	scraped   int
}

func (ys *serviceImpl) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*ChannelStats), nil
	}

	scrapeResult := ys.scrapeChannelStatistics(ctx, channelIDs)
	if len(scrapeResult.stats) == 0 && len(scrapeResult.failedIDs) > 0 {
		return nil, fmt.Errorf("get channel statistics: scraper failed for all %d channels", len(channelIDs))
	}

	ys.logger.Info("Channel statistics fetched (scraper)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(scrapeResult.stats)),
		slog.Int("scraped", scrapeResult.scraped),
		slog.Int("failed", len(scrapeResult.failedIDs)))

	return scrapeResult.stats, nil
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

func (ys *serviceImpl) resolveChannelTitle(channelID, fallbackTitle string) string {
	channelTitle := ys.getChannelName(channelID)
	if channelTitle != "" {
		return channelTitle
	}
	return fallbackTitle
}

func nonNegativeYouTubeCount(value int64) (uint64, bool) {
	if value < 0 {
		return 0, false
	}
	return uint64(value), true
}
