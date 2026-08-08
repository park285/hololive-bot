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

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	pollerruntime "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/pollers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
)

const defaultChannelPollerMaxResults = 10

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
	communityKeywords := []string{}
	pool := postgres.GetPool()
	tieringEnabled := scraperConfig.PollTiering.Enabled
	pollers := newYouTubeProducerPollerSet(scraperClient, liveStatusProvider, pool, communityKeywords)
	logLiveDiscoveryScope(logger, pollers.liveBatchEnabled, notificationChannelIDs, operationalChannelIDs)

	if registrations, ok := tryBuildTieredChannelPollerRegistrations(ctx, tieringEnabled, pool, &pollers, poll, polltarget.Targets{
		NotificationChannelIDs: notificationChannelIDs,
		OperationalChannelIDs:  operationalChannelIDs,
	}, logger); ok {
		return appendBackfillChannelPollerRegistrations(registrations, &pollers, scraperConfig.Backfill, notificationChannelIDs, operationalChannelIDs)
	}
	registrations := buildFlatYouTubeProducerChannelPollerRegistrations(&pollers, poll, notificationChannelIDs, operationalChannelIDs)
	return appendBackfillChannelPollerRegistrations(registrations, &pollers, scraperConfig.Backfill, notificationChannelIDs, operationalChannelIDs)
}

func appendBackfillChannelPollerRegistrations(
	registrations []providers.ChannelPollerRegistration,
	pollers *youTubeProducerPollerSet,
	backfill settings.ScraperBackfillConfig,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	if !backfill.Enabled {
		return registrations
	}
	if backfill.ShortsEnabled {
		shortsUnits := shortsWorstCaseRequestUnits()
		registrations = append(registrations, buildRegistration(&registrationSpec{
			Poller:                newNamedBackfillPoller("shorts_backfill", pollers.shorts),
			Priority:              scheduler.PriorityLow,
			Interval:              backfill.ShortsInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: shortsUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(shortsUnits, polling.BudgetBurstBackfill, polling.BudgetPriorityLow),
		}))
	}
	if backfill.CommunityEnabled {
		communityUnits := communityWorstCaseRequestUnits()
		registrations = append(registrations, buildRegistration(&registrationSpec{
			Poller:                newNamedBackfillPoller("community_backfill", pollers.community),
			Priority:              scheduler.PriorityLow,
			Interval:              backfill.CommunityInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: communityUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(communityUnits, polling.BudgetBurstBackfill, polling.BudgetPriorityLow),
		}))
	}
	if backfill.LiveEnabled {
		liveChannelIDs, liveTargetGroup := liveRegistrationTargets(pollers.liveBatchEnabled, notificationChannelIDs, operationalChannelIDs)
		registrations = appendLivePollerRegistrations(registrations, &livePollerRegistrationSpec{
			Name:           "live_backfill",
			Base:           newNamedBackfillPoller("live_backfill", pollers.live),
			BatchBase:      pollers.liveBatch,
			BatchEnabled:   pollers.liveBatchEnabled,
			Priority:       scheduler.PriorityLow,
			Interval:       backfill.LiveInterval,
			ChannelIDs:     liveChannelIDs,
			TargetGroup:    liveTargetGroup,
			BurstClass:     polling.BudgetBurstBackfill,
			BudgetPriority: polling.BudgetPriorityLow,
		})
	}
	return registrations
}

func buildFlatYouTubeProducerChannelPollerRegistrations(
	pollers *youTubeProducerPollerSet,
	poll settings.ScraperPoll,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	communityInterval := communityPrimaryPollInterval(poll)
	shortsUnits := shortsWorstCaseRequestUnits()
	communityUnits := communityWorstCaseRequestUnits()
	registrations := []providers.ChannelPollerRegistration{
		buildRegistration(&registrationSpec{
			Poller:                pollers.videos,
			Priority:              scheduler.PriorityNormal,
			Interval:              poll.Videos,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.FetchPageMaxAttempts,
			WorstCaseRequestUnits: videosWorstCaseRequestUnits(),
			BudgetProfile:         youtubeScraperBudgetProfile(videosWorstCaseRequestUnits(), polling.BudgetBurstPrimary, polling.BudgetPriorityNormal),
		}),
		buildRegistration(&registrationSpec{
			Poller:                pollers.shorts,
			Priority:              scheduler.PriorityLow,
			Interval:              poll.Shorts,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: shortsUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(shortsUnits, polling.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(scheduler.PriorityLow)),
		}),
		buildRegistration(&registrationSpec{
			Poller:                pollers.community,
			Priority:              scheduler.PriorityLow,
			Interval:              communityInterval,
			ChannelIDs:            notificationChannelIDs,
			TargetGroup:           providers.ChannelTargetGroupNotification,
			WorstCaseAttempts:     scraper.HighFrequencyChannelFetchPolicy.MaxAttempts,
			WorstCaseRequestUnits: communityUnits,
			BudgetProfile:         youtubeScraperBudgetProfile(communityUnits, polling.BudgetBurstPrimary, budgetPriorityFromRegistrationPriority(scheduler.PriorityLow)),
		}),
		buildStatsRegistration(pollers.stats, poll.Stats, operationalChannelIDs),
	}
	liveChannelIDs, liveTargetGroup := liveRegistrationTargets(pollers.liveBatchEnabled, notificationChannelIDs, operationalChannelIDs)
	return appendLivePollerRegistrations(registrations, &livePollerRegistrationSpec{
		Name:           "live",
		Base:           pollers.live,
		BatchBase:      pollers.liveBatch,
		BatchEnabled:   pollers.liveBatchEnabled,
		Priority:       scheduler.PriorityHigh,
		Interval:       poll.Live,
		ChannelIDs:     liveChannelIDs,
		TargetGroup:    liveTargetGroup,
		BurstClass:     polling.BudgetBurstPrimary,
		BudgetPriority: polling.BudgetPriorityHigh,
	})
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
	registrations := make([]providers.ChannelPollerRegistration, 0, 11)
	registrations = appendTieredNotificationRegistration(registrations, pollers.videos, targets, poll.Videos, scheduler.PriorityNormal, scraper.FetchPageMaxAttempts, videosWorstCaseRequestUnits(), youtubeScraperBudgetProfile(videosWorstCaseRequestUnits(), polling.BudgetBurstPrimary, polling.BudgetPriorityNormal), false)
	registrations = appendTieredNotificationRegistration(registrations, pollers.shorts, targets, poll.Shorts, scheduler.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, shortsWorstCaseRequestUnits(), youtubeScraperBudgetProfile(shortsWorstCaseRequestUnits(), polling.BudgetBurstPrimary, polling.BudgetPriorityLow), true)
	registrations = appendTieredNotificationRegistration(registrations, pollers.community, targets, communityPrimaryPollInterval(poll), scheduler.PriorityLow, scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, communityWorstCaseRequestUnits(), youtubeScraperBudgetProfile(communityWorstCaseRequestUnits(), polling.BudgetBurstPrimary, polling.BudgetPriorityLow), true)
	registrations = append(registrations, buildStatsRegistration(pollers.stats, poll.Stats, targets.OperationalChannelIDs))
	liveChannelIDs, liveTargetGroup := liveRegistrationTargets(pollers.liveBatchEnabled, targets.NotificationChannelIDs, targets.OperationalChannelIDs)
	registrations = appendLivePollerRegistrations(registrations, &livePollerRegistrationSpec{
		Name:           "live",
		Base:           pollers.live,
		BatchBase:      pollers.liveBatch,
		BatchEnabled:   pollers.liveBatchEnabled,
		Priority:       scheduler.PriorityHigh,
		Interval:       poll.Live,
		ChannelIDs:     liveChannelIDs,
		TargetGroup:    liveTargetGroup,
		BurstClass:     polling.BudgetBurstPrimary,
		BudgetPriority: polling.BudgetPriorityHigh,
	})
	return registrations
}

func liveRegistrationTargets(
	batchEnabled bool,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) ([]string, providers.ChannelTargetGroup) {
	if batchEnabled {
		return operationalChannelIDs, providers.ChannelTargetGroupOperational
	}
	return notificationChannelIDs, providers.ChannelTargetGroupNotification
}

func logLiveDiscoveryScope(
	logger *slog.Logger,
	batchEnabled bool,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) {
	if logger == nil || batchEnabled || len(notificationChannelIDs) == len(operationalChannelIDs) {
		return
	}
	logger.Warn("youtube_producer_live_discovery_subscription_fallback",
		slog.Int("notification_target_channels", len(notificationChannelIDs)),
		slog.Int("operational_target_channels", len(operationalChannelIDs)),
	)
}

func communityPrimaryPollInterval(poll settings.ScraperPoll) time.Duration {
	if poll.Shorts > 0 {
		return poll.Shorts
	}
	return poll.Community
}

func appendTieredNotificationRegistration(
	registrations []providers.ChannelPollerRegistration,
	pollerInstance scheduler.Poller,
	targets *polltarget.TieredTargets,
	baseInterval time.Duration,
	basePriority scheduler.Priority,
	worstCaseAttempts int,
	worstCaseRequestUnits float64,
	budgetProfile polling.BudgetProfile,
	deriveBudgetPriorityFromRegistration bool,
) []providers.ChannelPollerRegistration {
	registrations = append(registrations, newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupActive, basePriority, baseInterval, targets.ActiveNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration))
	priority := scheduler.PriorityNormal
	if basePriority == scheduler.PriorityLow {
		priority = scheduler.PriorityLow
	}
	registrations = append(
		registrations,
		newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupWarm, priority, baseInterval*2, targets.WarmNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration),
		newTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupCold, scheduler.PriorityLow, baseInterval*6, targets.ColdNotificationChannelIDs, worstCaseAttempts, worstCaseRequestUnits, budgetProfile, deriveBudgetPriorityFromRegistration),
	)
	return registrations
}

func newTieredNotificationRegistration(
	pollerInstance scheduler.Poller,
	targetGroup providers.ChannelTargetGroup,
	priority scheduler.Priority,
	interval time.Duration,
	channelIDs []string,
	worstCaseAttempts int,
	worstCaseRequestUnits float64,
	budgetProfile polling.BudgetProfile,
	deriveBudgetPriorityFromRegistration bool,
) providers.ChannelPollerRegistration {
	if deriveBudgetPriorityFromRegistration {
		budgetProfile = budgetProfileWithRegistrationPriority(budgetProfile, priority)
	}
	return buildRegistration(&registrationSpec{
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
