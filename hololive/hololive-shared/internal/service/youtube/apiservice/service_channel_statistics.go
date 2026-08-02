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
	"slices"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/internal/service/fallback"
	ytcontract "github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/park285/shared-go/pkg/json"
)

const (
	channelStatsCacheKeyPrefix = "youtube:channel_stats:"
	channelStatsCacheTTL       = 5 * time.Minute
)

type channelStatsScrapeResult struct {
	stats     map[string]*ytcontract.ChannelStats
	failedIDs []string
	scraped   int
}

func (ys *serviceImpl) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ytcontract.ChannelStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*ytcontract.ChannelStats), nil
	}

	stats, missing := ys.cachedChannelStatistics(ctx, channelIDs)
	if len(missing) == 0 {
		return stats, nil
	}

	scrapeResult := ys.scrapeChannelStatistics(ctx, missing)
	if len(stats) == 0 && len(scrapeResult.stats) == 0 && len(scrapeResult.failedIDs) > 0 {
		return nil, fmt.Errorf("get channel statistics: scraper failed for all %d channels", len(missing))
	}

	ys.storeChannelStatistics(ctx, scrapeResult.stats)
	maps.Copy(stats, scrapeResult.stats)

	ys.logger.Debug("Channel statistics fetched (scraper)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(stats)),
		slog.Int("scraped", scrapeResult.scraped),
		slog.Int("failed", len(scrapeResult.failedIDs)))

	return stats, nil
}

func channelStatsCacheKey(channelID string) string {
	return channelStatsCacheKeyPrefix + channelID
}

func (ys *serviceImpl) cachedChannelStatistics(ctx context.Context, channelIDs []string) (cached map[string]*ytcontract.ChannelStats, missingIDs []string) {
	stats := make(map[string]*ytcontract.ChannelStats, len(channelIDs))
	if ys.cache == nil {
		return stats, slices.Clone(channelIDs)
	}

	missing := make([]string, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channelStats, ok := ys.cachedChannelStats(ctx, channelID)
		if !ok {
			missing = append(missing, channelID)
			continue
		}
		stats[channelID] = channelStats
	}

	return stats, missing
}

func (ys *serviceImpl) cachedChannelStats(ctx context.Context, channelID string) (*ytcontract.ChannelStats, bool) {
	raw, hit, err := ys.cache.GetString(ctx, channelStatsCacheKey(channelID))
	if err != nil || !hit || raw == "" {
		return nil, false
	}

	var stats ytcontract.ChannelStats
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		ys.logger.Debug("Channel statistics cache decode failed",
			slog.String("channel", channelID),
			slog.Any("error", err))
		return nil, false
	}

	return &stats, true
}

// 캐시 기록 실패는 다음 호출에서 재스크레이프로 복구되므로 응답을 막지 않는다.
func (ys *serviceImpl) storeChannelStatistics(ctx context.Context, stats map[string]*ytcontract.ChannelStats) {
	if ys.cache == nil || len(stats) == 0 {
		return
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ytDefaults.CacheSaveTimeout)
	defer cancel()

	for channelID, channelStats := range stats {
		if err := ys.cache.Set(saveCtx, channelStatsCacheKey(channelID), channelStats, channelStatsCacheTTL); err != nil {
			ys.logger.Debug("Channel statistics cache write failed",
				slog.String("channel", channelID),
				slog.Any("error", err))
		}
	}
}

func (ys *serviceImpl) scrapeChannelStatistics(ctx context.Context, channelIDs []string) channelStatsScrapeResult {
	result := channelStatsScrapeResult{
		stats: make(map[string]*ytcontract.ChannelStats),
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
	ys.logger.Debug("Scraper phase completed",
		slog.Int("total", len(channelIDs)),
		slog.Int("scraped", result.scraped),
		slog.Int("failed", len(result.failedIDs)))
	return result
}

func (ys *serviceImpl) scrapeSingleChannelStatistics(ctx context.Context, channelID string) (*ytcontract.ChannelStats, error) {
	stats, err := ys.scraper.GetChannelStats(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("scraper channel stats for %s: %w", channelID, err)
	}
	return ys.channelStatsFromScraped(channelID, stats)
}

func (ys *serviceImpl) channelStatsFromScraped(channelID string, stats *parser.ChannelStats) (*ytcontract.ChannelStats, error) {
	subscriberCount, videoCount, viewCount, err := validatedScrapedChannelCounts(channelID, stats)
	if err != nil {
		return nil, err
	}
	return &ytcontract.ChannelStats{
		ChannelID:       stats.ChannelID,
		ChannelTitle:    ys.resolveChannelTitle(channelID, stats.Handle),
		SubscriberCount: subscriberCount,
		VideoCount:      videoCount,
		ViewCount:       viewCount,
		Timestamp:       time.Now(),
	}, nil
}

func validatedScrapedChannelCounts(channelID string, stats *parser.ChannelStats) (subscriberCount, videoCount, viewCount uint64, err error) {
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
