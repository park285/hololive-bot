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
	videos           poller.Poller
	shorts           poller.Poller
	community        poller.Poller
	stats            poller.Poller
	live             poller.Poller
	liveBatch        *poller.LivePoller
	liveBatchEnabled bool
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
	livePoller := poller.NewLivePollerWithStatusProvider(liveStatusProvider, scraperClient, db)
	return youTubeProducerPollerSet{
		videos:           poller.NewVideosPoller(scraperClient, db, maxResults),
		shorts:           poller.NewShortsPoller(scraperClient, db, maxResults, routeDecider, inlineResolveMissingPublishedAt),
		community:        poller.NewCommunityPoller(scraperClient, db, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt),
		stats:            poller.NewChannelStatsPoller(scraperClient, db),
		live:             livePoller,
		liveBatch:        livePoller,
		liveBatchEnabled: liveStatusProvider != nil,
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
		shortsUnits := shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)
		registrations = append(registrations, buildRegistration(registrationSpec{
			Poller:                newNamedBackfillPoller("shorts_backfill", pollers.shorts),
			Priority:              poller.PriorityLow,
			Interval:              backfill.ShortsInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: shortsUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(shortsUnits, poller.BudgetBurstBackfill, poller.BudgetPriorityLow),
		}))
	}
	if backfill.CommunityEnabled {
		communityUnits := communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)
		registrations = append(registrations, buildRegistration(registrationSpec{
			Poller:                newNamedBackfillPoller("community_backfill", pollers.community),
			Priority:              poller.PriorityLow,
			Interval:              backfill.CommunityInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: communityUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(communityUnits, poller.BudgetBurstBackfill, poller.BudgetPriorityLow),
		}))
	}
	if backfill.LiveEnabled {
		registrations = appendLivePollerRegistrations(registrations, livePollerRegistrationSpec{
			Name:           "live_backfill",
			Base:           newNamedBackfillPoller("live_backfill", pollers.live),
			BatchBase:      pollers.liveBatch,
			BatchEnabled:   pollers.liveBatchEnabled,
			Priority:       poller.PriorityLow,
			Interval:       backfill.LiveInterval,
			ChannelIDs:     notificationChannelIDs,
			TargetGroup:    providers.ChannelTargetGroupNotification,
			BurstClass:     poller.BudgetBurstBackfill,
			BudgetPriority: poller.BudgetPriorityLow,
		})
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
	communityInterval := communityPrimaryPollInterval(poll)
	shortsUnits := shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)
	communityUnits := communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)
	registrations := []providers.ChannelPollerRegistration{
		buildRegistration(registrationSpec{
			Poller:                pollers.videos,
			Priority:              poller.PriorityNormal,
			Interval:              poll.Videos,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.FetchPageMaxAttempts,
			WorstCaseRequestUnits: videosWorstCaseRequestUnits(),
			BudgetProfile:         youtubeScraperBudgetProfile(videosWorstCaseRequestUnits(), poller.BudgetBurstPrimary, poller.BudgetPriorityNormal),
		}),
		buildRegistration(registrationSpec{
			Poller:                pollers.shorts,
			Priority:              poller.PriorityLow,
			Interval:              poll.Shorts,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: shortsUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(shortsUnits, poller.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(poller.PriorityLow)),
		}),
		buildRegistration(registrationSpec{
			Poller:                pollers.community,
			Priority:              poller.PriorityLow,
			Interval:              communityInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: communityUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(communityUnits, poller.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(poller.PriorityLow)),
		}),
		buildStatsRegistration(pollers.stats, poll.Stats, statsChannelIDs),
	}
	return appendLivePollerRegistrations(registrations, livePollerRegistrationSpec{
		Name:           "live",
		Base:           pollers.live,
		BatchBase:      pollers.liveBatch,
		BatchEnabled:   pollers.liveBatchEnabled,
		Priority:       poller.PriorityHigh,
		Interval:       poll.Live,
		ChannelIDs:     notificationChannelIDs,
		TargetGroup:    providers.ChannelTargetGroupNotification,
		BurstClass:     poller.BudgetBurstPrimary,
		BudgetPriority: poller.BudgetPriorityHigh,
	})
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
	registrations = appendTieredNotificationRegistration(registrations, pollers.community, targets, communityPrimaryPollInterval(poll), poller.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), youtubeScraperBudgetProfile(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults), poller.BudgetBurstPrimary, poller.BudgetPriorityLow), true)
	registrations = append(registrations, buildStatsRegistration(pollers.stats, poll.Stats, targets.StatsChannelIDs))
	registrations = appendLivePollerRegistrations(registrations, livePollerRegistrationSpec{
		Name:           "live",
		Base:           pollers.live,
		BatchBase:      pollers.liveBatch,
		BatchEnabled:   pollers.liveBatchEnabled,
		Priority:       poller.PriorityHigh,
		Interval:       poll.Live,
		ChannelIDs:     targets.NotificationChannelIDs,
		TargetGroup:    providers.ChannelTargetGroupNotification,
		BurstClass:     poller.BudgetBurstPrimary,
		BudgetPriority: poller.BudgetPriorityHigh,
	})
	return registrations
}

func communityPrimaryPollInterval(poll config.ScraperPoll) time.Duration {
	if poll.Shorts > 0 {
		return poll.Shorts
	}
	return poll.Community
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
	return buildRegistration(registrationSpec{
		Poller:                pollerInstance,
		Priority:              priority,
		Interval:              interval,
		ChannelIDs:            channelIDs,
		TargetGroup:           targetGroup,
		WorstCaseAttempts:     worstCaseAttempts,
		WorstCaseRequestUnits: worstCaseRequestUnits,
		BudgetProfile:         budgetProfile,
	})
}

type registrationSpec struct {
	Poller                poller.Poller
	Priority              poller.Priority
	Interval              time.Duration
	ChannelIDs            []string
	TargetGroup           providers.ChannelTargetGroup
	WorstCaseAttempts     int
	WorstCaseRequestUnits float64
	BudgetProfile         poller.BudgetProfile
}

func buildRegistration(spec registrationSpec) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(spec.Poller, spec.Priority, spec.Interval).
		WithChannelIDs(spec.ChannelIDs).
		WithTargetGroup(spec.TargetGroup).
		WithWorstCaseAttempts(spec.WorstCaseAttempts).
		WithWorstCaseRequestUnitsPerRun(spec.WorstCaseRequestUnits).
		WithBudgetProfile(spec.BudgetProfile)
}

func buildStatsRegistration(statsPoller poller.Poller, interval time.Duration, channelIDs []string) providers.ChannelPollerRegistration {
	return buildRegistration(registrationSpec{
		Poller:                statsPoller,
		Priority:              poller.PriorityLow,
		Interval:              interval,
		ChannelIDs:            channelIDs,
		TargetGroup:           providers.ChannelTargetGroupStats,
		WorstCaseAttempts:     scraper.FetchPageMaxAttempts,
		WorstCaseRequestUnits: channelStatsWorstCaseRequestUnits(),
		BudgetProfile:         youtubeScraperBudgetProfile(channelStatsWorstCaseRequestUnits(), poller.BudgetBurstPrimary, poller.BudgetPriorityLow),
	})
}

func videosWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 3)
}

func channelStatsWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 2)
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
