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

package polling

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"

	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	pollerruntime "github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
)

func buildYouTubeProducerChannelPollerRegistrations(
	ctx context.Context,
	postgres database.Client,
	scraperConfig *settings.ScraperConfig,
	sharedRL *ratelimiter.RateLimiter,
	cacheClient cache.Client,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	if scraperConfig == nil {
		scraperConfig = &settings.ScraperConfig{}
	}
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		ctx,
		postgres,
		scraperConfig,
		buildSharedYouTubeProducerClient(scraperConfig, cacheClient, sharedRL),
		nil,
		notificationChannelIDs,
		operationalChannelIDs,
		slog.Default(),
	)
}

func buildYouTubeProducerChannelPollerRegistrationsWithClient(
	ctx context.Context,
	postgres database.Client,
	scraperConfig *settings.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider pollerruntime.LiveStatusProvider,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
	logger *slog.Logger,
) []providers.ChannelPollerRegistration {
	if scraperConfig == nil {
		scraperConfig = &settings.ScraperConfig{}
	}
	poll := scraperConfig.PollOrDefault()
	pool := postgres.GetPool()
	pollers := newYouTubeProducerPollerSet(scraperClient, liveStatusProvider, pool)
	return buildFlatYouTubeProducerChannelPollerRegistrations(&pollers, poll, notificationChannelIDs, operationalChannelIDs)
}

func appendBackfillChannelPollerRegistrations(
	registrations []providers.ChannelPollerRegistration,
	_ *youTubeProducerPollerSet,
	_ settings.ScraperBackfillConfig,
	_ []string,
	_ []string,
) []providers.ChannelPollerRegistration {
	return registrations
}

func buildFlatYouTubeProducerChannelPollerRegistrations(
	pollers *youTubeProducerPollerSet,
	poll settings.ScraperPoll,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return []providers.ChannelPollerRegistration{
		buildStatsRegistration(pollers.stats, poll.Stats, operationalChannelIDs),
	}
}

func tryBuildTieredChannelPollerRegistrations(
	ctx context.Context,
	enabled bool,
	pool *pgxpool.Pool,
	pollers *youTubeProducerPollerSet,
	poll settings.ScraperPoll,
	targets polltarget.Targets,
	logger *slog.Logger,
) ([]providers.ChannelPollerRegistration, bool) {
	if !enabled {
		return nil, false
	}
	tieredTargets, tierErr := polltarget.ClassifyByActivity(ctx, pool, targets, time.Now())
	if tierErr != nil {
		if logger != nil {
			logger.Warn("youtube_producer_poll_tiering_fallback_to_flat", slog.Any("error", tierErr))
		}
		return nil, false
	}
	return buildTieredYouTubeProducerChannelPollerRegistrations(pollers, poll, &tieredTargets), true
}

func buildTieredYouTubeProducerChannelPollerRegistrations(
	pollers *youTubeProducerPollerSet,
	poll settings.ScraperPoll,
	targets *polltarget.TieredTargets,
) []providers.ChannelPollerRegistration {
	return []providers.ChannelPollerRegistration{
		buildStatsRegistration(pollers.stats, poll.Stats, targets.OperationalChannelIDs),
	}
}
