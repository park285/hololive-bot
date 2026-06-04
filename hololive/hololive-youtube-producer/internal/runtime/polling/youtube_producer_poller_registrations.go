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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
)

const defaultChannelPollerMaxResults = 10

func buildYouTubeProducerChannelPollerRegistrations(
	postgres database.Client,
	scraperConfig config.ScraperConfig,
	sharedRL *scraper.RateLimiter,
	cacheClient cache.Client,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		postgres,
		scraperConfig,
		buildSharedYouTubeProducerClient(scraperConfig, cacheClient, sharedRL),
		nil,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func buildYouTubeProducerChannelPollerRegistrationsWithClient(
	postgres database.Client,
	scraperConfig config.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	poll := scraperConfig.PollOrDefault()
	resolverConfig := publishedat.EffectiveConfig(scraperConfig)
	inlineResolveMissingPublishedAt := routeDecider != nil && !resolverConfig.Enabled
	communityKeywords := []string{}
	pool := postgres.GetPool()
	maxResults := defaultChannelPollerMaxResults
	tieringEnabled := scraperConfig.PollTiering.Enabled
	pollers := newYouTubeProducerPollerSet(scraperClient, liveStatusProvider, pool, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)

	if registrations, ok := tryBuildTieredChannelPollerRegistrations(tieringEnabled, pool, pollers, poll, polltarget.Targets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
	}, inlineResolveMissingPublishedAt, maxResults); ok {
		return appendBackfillChannelPollerRegistrations(registrations, pollers, scraperConfig.Backfill, notificationChannelIDs, inlineResolveMissingPublishedAt, maxResults)
	}
	registrations := buildFlatYouTubeProducerChannelPollerRegistrations(pollers, poll, notificationChannelIDs, statsChannelIDs, inlineResolveMissingPublishedAt, maxResults)
	return appendBackfillChannelPollerRegistrations(registrations, pollers, scraperConfig.Backfill, notificationChannelIDs, inlineResolveMissingPublishedAt, maxResults)
}

type youTubeProducerPollerSet struct {
	videos    poller.Poller
	shorts    poller.Poller
	community poller.Poller
	stats     poller.Poller
	live      poller.Poller
}

type namedBackfillPoller struct {
	name string
	base poller.Poller
}

func newNamedBackfillPoller(name string, base poller.Poller) poller.Poller {
	return namedBackfillPoller{name: name, base: base}
}

func (p namedBackfillPoller) Poll(ctx context.Context, channelID string) error {
	return p.base.Poll(ctx, channelID)
}

func (p namedBackfillPoller) Name() string {
	return p.name
}

func (p namedBackfillPoller) SetProxyEnabled(enabled bool) bool {
	proxyPoller, ok := p.base.(interface {
		SetProxyEnabled(bool) bool
	})
	if !ok {
		return false
	}
	return proxyPoller.SetProxyEnabled(enabled)
}

func (p namedBackfillPoller) ProxyEnabled() bool {
	proxyPoller, ok := p.base.(interface {
		ProxyEnabled() bool
	})
	return ok && proxyPoller.ProxyEnabled()
}

func newYouTubeProducerPollerSet(
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	db any,
	maxResults int,
	communityKeywords []string,
	routeDecider poller.NotificationRouteDecider,
	inlineResolveMissingPublishedAt bool,
) youTubeProducerPollerSet {
	return youTubeProducerPollerSet{
		videos:    poller.NewVideosPoller(scraperClient, db, maxResults),
		shorts:    poller.NewShortsPoller(scraperClient, db, maxResults, routeDecider, inlineResolveMissingPublishedAt),
		community: poller.NewCommunityPoller(scraperClient, db, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt),
		stats:     poller.NewChannelStatsPoller(scraperClient, db),
		live:      poller.NewLivePollerWithStatusProvider(liveStatusProvider, scraperClient, db),
	}
}

func youtubeScraperBudgetProfile(units float64, class poller.BudgetBurstClass, priority poller.BudgetPriority) poller.BudgetProfile {
	return poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceYouTubeScraper: units,
			poller.BudgetSourcePostgresWrite:  1,
		},
		BurstClass: class,
		Priority:   priority,
	}
}

func holodexLiveBudgetProfile(class poller.BudgetBurstClass, priority poller.BudgetPriority) poller.BudgetProfile {
	return poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceHolodexLive:   1,
			poller.BudgetSourcePostgresWrite: 1,
		},
		BurstClass: class,
		Priority:   priority,
	}
}

func budgetProfileWithRegistrationPriority(profile poller.BudgetProfile, priority poller.Priority) poller.BudgetProfile {
	profile.Priority = budgetPriorityFromRegistrationPriority(priority)
	return profile
}

func budgetPriorityFromRegistrationPriority(priority poller.Priority) poller.BudgetPriority {
	switch priority {
	case poller.PriorityHigh, poller.PriorityBoost:
		return poller.BudgetPriorityHigh
	case poller.PriorityLow:
		return poller.BudgetPriorityLow
	default:
		return poller.BudgetPriorityNormal
	}
}

func appendBackfillChannelPollerRegistrations(
	registrations []providers.ChannelPollerRegistration,
	pollers youTubeProducerPollerSet,
	backfill config.ScraperBackfillConfig,
	notificationChannelIDs []string,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) []providers.ChannelPollerRegistration {
	if !backfill.Enabled {
		return registrations
	}
	if backfill.ShortsEnabled {
		registrations = append(registrations, providers.NewChannelPollerRegistration(newNamedBackfillPoller("shorts_backfill", pollers.shorts), poller.PriorityLow, backfill.ShortsInterval).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)).
			WithBudgetProfile(youtubeScraperBudgetProfile(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstBackfill, poller.BudgetPriorityLow)))
	}
	if backfill.CommunityEnabled {
		registrations = append(registrations, providers.NewChannelPollerRegistration(newNamedBackfillPoller("community_backfill", pollers.community), poller.PriorityLow, backfill.CommunityInterval).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)).
			WithBudgetProfile(youtubeScraperBudgetProfile(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstBackfill, poller.BudgetPriorityLow)))
	}
	if backfill.LiveEnabled {
		registrations = append(registrations, providers.NewChannelPollerRegistration(newNamedBackfillPoller("live_backfill", pollers.live), poller.PriorityLow, backfill.LiveInterval).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
			WithBudgetProfile(holodexLiveBudgetProfile(poller.BudgetBurstBackfill, poller.BudgetPriorityLow)))
	}
	return registrations
}

func buildFlatYouTubeProducerChannelPollerRegistrations(
	pollers youTubeProducerPollerSet,
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
			WithWorstCaseRequestUnitsPerRun(videosWorstCaseRequestUnits()).
			WithBudgetProfile(youtubeScraperBudgetProfile(videosWorstCaseRequestUnits(), poller.BudgetBurstPrimary, poller.BudgetPriorityNormal)),
		providers.NewChannelPollerRegistration(pollers.shorts, poller.PriorityLow, poll.Shorts).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)).
			WithBudgetProfile(youtubeScraperBudgetProfile(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(poller.PriorityLow))),
		providers.NewChannelPollerRegistration(pollers.community, poller.PriorityLow, poll.Community).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)).
			WithBudgetProfile(youtubeScraperBudgetProfile(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(poller.PriorityLow))),
		providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
			WithChannelIDs(statsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
			WithBudgetProfile(youtubeScraperBudgetProfile(float64(scraper.FetchPageMaxAttempts), poller.BudgetBurstPrimary, poller.BudgetPriorityLow)),
		providers.NewChannelPollerRegistration(pollers.live, poller.PriorityHigh, poll.Live).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
			WithBudgetProfile(holodexLiveBudgetProfile(poller.BudgetBurstPrimary, poller.BudgetPriorityHigh)),
	}
}

func tryBuildTieredChannelPollerRegistrations(
	enabled bool,
	pool *pgxpool.Pool,
	pollers youTubeProducerPollerSet,
	poll config.ScraperPoll,
	targets polltarget.Targets,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) ([]providers.ChannelPollerRegistration, bool) {
	if !enabled {
		return nil, false
	}
	tieredTargets, tierErr := polltarget.ClassifyByActivity(context.Background(), pool, targets, time.Now())
	if tierErr != nil {
		return nil, false
	}
	return buildTieredYouTubeProducerChannelPollerRegistrations(pollers, poll, tieredTargets, inlineResolveMissingPublishedAt, maxResults), true
}

func buildTieredYouTubeProducerChannelPollerRegistrations(
	pollers youTubeProducerPollerSet,
	poll config.ScraperPoll,
	targets polltarget.TieredTargets,
	inlineResolveMissingPublishedAt bool,
	maxResults int,
) []providers.ChannelPollerRegistration {
	registrations := make([]providers.ChannelPollerRegistration, 0, 11)
	registrations = appendTieredNotificationRegistration(registrations, pollers.videos, targets, poll.Videos, poller.PriorityNormal, scraper.FetchPageMaxAttempts, videosWorstCaseRequestUnits(), youtubeScraperBudgetProfile(videosWorstCaseRequestUnits(), poller.BudgetBurstPrimary, poller.BudgetPriorityNormal), false)
	registrations = appendTieredNotificationRegistration(registrations, pollers.shorts, targets, poll.Shorts, poller.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), youtubeScraperBudgetProfile(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstPrimary, poller.BudgetPriorityLow), true)
	registrations = appendTieredNotificationRegistration(registrations, pollers.community, targets, poll.Community, poller.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), youtubeScraperBudgetProfile(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstPrimary, poller.BudgetPriorityLow), true)
	registrations = append(registrations, providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
		WithChannelIDs(targets.StatsChannelIDs).
		WithTargetGroup(providers.ChannelTargetGroupStats).
		WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
		WithBudgetProfile(youtubeScraperBudgetProfile(float64(scraper.FetchPageMaxAttempts), poller.BudgetBurstPrimary, poller.BudgetPriorityLow)))
	registrations = append(registrations, providers.NewChannelPollerRegistration(pollers.live, poller.PriorityHigh, poll.Live).
		WithChannelIDs(targets.NotificationChannelIDs).
		WithTargetGroup(providers.ChannelTargetGroupNotification).
		WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
		WithBudgetProfile(holodexLiveBudgetProfile(poller.BudgetBurstPrimary, poller.BudgetPriorityHigh)))
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
	budgetProfile poller.BudgetProfile,
	deriveBudgetPriorityFromRegistration bool,
) []providers.ChannelPollerRegistration {
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupActive, basePriority, baseInterval, targets.ActiveNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration))
	priority := poller.PriorityNormal
	if basePriority == poller.PriorityLow {
		priority = poller.PriorityLow
	}
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupWarm, priority, baseInterval*2, targets.WarmNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration))
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupCold, poller.PriorityLow, baseInterval*6, targets.ColdNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration))
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
	budgetProfile poller.BudgetProfile,
	deriveBudgetPriorityFromRegistration bool,
) providers.ChannelPollerRegistration {
	if deriveBudgetPriorityFromRegistration {
		budgetProfile = budgetProfileWithRegistrationPriority(budgetProfile, priority)
	}
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(targetGroup).
		WithWorstCaseAttempts(worstCaseAttempts).
		WithWorstCaseRequestUnitsPerRun(worstCaseRequestUnits).
		WithBudgetProfile(budgetProfile)
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
