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
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/polltarget"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/publishedat"
	"gorm.io/gorm"
)

const defaultChannelPollerMaxResults = 10

func buildStreamIngesterChannelPollerRegistrations(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc cache.Client,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildStreamIngesterChannelPollerRegistrationsWithClient(
		postgres,
		scraperCfg,
		buildSharedYouTubeScraperClient(scraperCfg, cacheSvc, sharedRL),
		nil,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func buildStreamIngesterChannelPollerRegistrationsWithClient(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	poll := scraperCfg.PollOrDefault()
	resolverCfg := publishedat.EffectiveConfig(scraperCfg)
	inlineResolveMissingPublishedAt := routeDecider != nil && !resolverCfg.Enabled
	communityKeywords := []string{}
	db := postgres.GetGormDB()
	maxResults := defaultChannelPollerMaxResults
	tieringEnabled := scraperCfg.PollTiering.Enabled
	pollers := newStreamIngesterPollerSet(scraperClient, liveStatusProvider, db, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)

	if registrations, ok := tryBuildTieredChannelPollerRegistrations(tieringEnabled, db, pollers, poll, polltarget.Targets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
	}, inlineResolveMissingPublishedAt, maxResults); ok {
		return registrations
	}
	return buildFlatStreamIngesterChannelPollerRegistrations(pollers, poll, notificationChannelIDs, statsChannelIDs, inlineResolveMissingPublishedAt, maxResults)
}

type streamIngesterPollerSet struct {
	videos    poller.Poller
	shorts    poller.Poller
	community poller.Poller
	stats     poller.Poller
	live      poller.Poller
}

func newStreamIngesterPollerSet(
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	db *gorm.DB,
	maxResults int,
	communityKeywords []string,
	routeDecider poller.NotificationRouteDecider,
	inlineResolveMissingPublishedAt bool,
) streamIngesterPollerSet {
	return streamIngesterPollerSet{
		videos:    poller.NewVideosPoller(scraperClient, db, maxResults),
		shorts:    poller.NewShortsPoller(scraperClient, db, maxResults, routeDecider, inlineResolveMissingPublishedAt),
		community: poller.NewCommunityPoller(scraperClient, db, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt),
		stats:     poller.NewChannelStatsPoller(scraperClient, db),
		live:      poller.NewLivePollerWithStatusProvider(liveStatusProvider, scraperClient, db),
	}
}

func buildFlatStreamIngesterChannelPollerRegistrations(
	pollers streamIngesterPollerSet,
	poll config.ScraperPoll,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) []providers.ChannelPollerRegistration {
	return []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(pollers.videos, poller.PriorityNormal, poll.Videos).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(videosWorstCaseRequestUnits()),
		providers.NewChannelPollerRegistration(pollers.shorts, poller.PriorityLow, poll.Shorts).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)),
		providers.NewChannelPollerRegistration(pollers.community, poller.PriorityLow, poll.Community).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)),
		providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
			WithChannelIDs(statsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),
		providers.NewChannelPollerRegistration(pollers.live, poller.PriorityHigh, poll.Live).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),
	}
}

func tryBuildTieredChannelPollerRegistrations(
	enabled bool,
	db *gorm.DB,
	pollers streamIngesterPollerSet,
	poll config.ScraperPoll,
	targets polltarget.Targets,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) ([]providers.ChannelPollerRegistration, bool) {
	if !enabled {
		return nil, false
	}
	tieredTargets, tierErr := polltarget.ClassifyByActivity(context.Background(), db, targets, time.Now())
	if tierErr != nil {
		return nil, false
	}
	return buildTieredStreamIngesterChannelPollerRegistrations(pollers, poll, tieredTargets, inlineResolveMissingPublishedAt, maxResults), true
}

func buildTieredStreamIngesterChannelPollerRegistrations(
	pollers streamIngesterPollerSet,
	poll config.ScraperPoll,
	targets polltarget.TieredTargets,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) []providers.ChannelPollerRegistration {
	registrations := make([]providers.ChannelPollerRegistration, 0, 11)
	registrations = appendTieredNotificationRegistration(registrations, pollers.videos, targets, poll.Videos, poller.PriorityNormal, scraper.FetchPageMaxAttempts, videosWorstCaseRequestUnits())
	registrations = appendTieredNotificationRegistration(registrations, pollers.shorts, targets, poll.Shorts, poller.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults))
	registrations = appendTieredNotificationRegistration(registrations, pollers.community, targets, poll.Community, poller.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults))
	registrations = append(registrations, providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
		WithChannelIDs(targets.StatsChannelIDs).
		WithTargetGroup(providers.ChannelTargetGroupStats).
		WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)))
	registrations = append(registrations, providers.NewChannelPollerRegistration(pollers.live, poller.PriorityHigh, poll.Live).
		WithChannelIDs(targets.NotificationChannelIDs).
		WithTargetGroup(providers.ChannelTargetGroupNotification).
		WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)))
	return registrations
}

func appendTieredNotificationRegistration(
	registrations []providers.ChannelPollerRegistration,
	pollerInstance poller.Poller,
	targets polltarget.TieredTargets,
	baseInterval time.Duration,
	basePriority poller.Priority,
	worstCaseAttempts int,
	worstCaseRequestUnits float64,
) []providers.ChannelPollerRegistration {
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupActive, basePriority, baseInterval, targets.ActiveNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits))
	priority := poller.PriorityNormal
	if basePriority == poller.PriorityLow {
		priority = poller.PriorityLow
	}
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupWarm, priority, baseInterval*2, targets.WarmNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits))
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupCold, poller.PriorityLow, baseInterval*6, targets.ColdNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits))
	return registrations
}

func newTieredNotificationRegistration(
	pollerInstance poller.Poller,
	targetGroup providers.ChannelTargetGroup,
	priority poller.Priority,
	interval time.Duration,
	channelIDs []string,
	worstCaseAttempts int,
	worstCaseRequestUnits float64,
) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(targetGroup).
		WithWorstCaseAttempts(worstCaseAttempts).
		WithWorstCaseRequestUnitsPerRun(worstCaseRequestUnits)
}

func videosWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 3)
}

func shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt bool, maxResults int) float64 {
	units := 1.0
	if inlineResolveMissingPublishedAt {
		units += float64(scraper.FetchPageMaxAttempts)
		units += float64(maxResults)
	}
	return units
}

func communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt bool, maxResults int) float64 {
	units := 1.0
	if inlineResolveMissingPublishedAt {
		units += float64(maxResults)
	}
	return units
}
