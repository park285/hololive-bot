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
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type upcomingAPIFallbackResult struct {
	streams            []*domain.Stream
	quotaCost          int
	successfulChannels int
	successfulIDs      []string
	failedIDs          []string
	failures           []upcomingScrapeFailure
}

type upcomingScrapeResult struct {
	streams   []*domain.Stream
	failedIDs []string
	scraped   int
	failures  []upcomingScrapeFailure
}

type upcomingScrapeFailure struct {
	ChannelID  string
	Source     string
	Reason     string
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

// 스크래퍼를 우선 사용하고, 실패한 채널만 YouTube API로 폴백합니다.
func (ys *serviceImpl) GetUpcomingStreams(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	channelIDs = ys.limitUpcomingChannelIDs(channelIDs)
	cacheKey := upcomingCacheKey(channelIDs)
	if cached, found := ys.getCachedUpcomingStreams(ctx, cacheKey); found {
		return cached, nil
	}

	scrapeResult := ys.scrapeUpcomingStreams(ctx, channelIDs)
	allStreams, err := ys.completeUpcomingAPIFallback(ctx, cacheKey, &scrapeResult)
	if err != nil {
		return nil, err
	}

	ys.cache.SetStreams(ctx, cacheKey, allStreams, ytDefaults.CacheExpiration)
	ys.logger.Info("Upcoming streams fetch completed (scraper+API)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("streams", len(allStreams)),
		slog.Int("scraped", scrapeResult.scraped),
		slog.Int("api_fallback", len(scrapeResult.failedIDs)))

	return allStreams, nil
}

func (ys *serviceImpl) limitUpcomingChannelIDs(channelIDs []string) []string {
	if len(channelIDs) <= ytDefaults.MaxChannelsPerCall {
		return channelIDs
	}

	ys.logger.Warn("Too many channels requested, limiting to max",
		slog.Int("requested", len(channelIDs)),
		slog.Int("limited", ytDefaults.MaxChannelsPerCall))
	return channelIDs[:ytDefaults.MaxChannelsPerCall]
}

func (ys *serviceImpl) getCachedUpcomingStreams(ctx context.Context, cacheKey string) ([]*domain.Stream, bool) {
	cached, found := ys.cache.GetStreams(ctx, cacheKey)
	if !found {
		return nil, false
	}

	ys.logger.Debug("YouTube cache hit (backup avoided)", slog.Int("streams", len(cached)))
	return cached, true
}
