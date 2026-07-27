package polltarget

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func buildYouTubeProducerChannelPollerRegistrations(
	postgres database.Client,
	scraperConfig *settings.ScraperConfig,
	_ *ratelimiter.RateLimiter,
	_ cache.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	poll := scraperConfig.PollOrDefault()
	pollers := testPollerSet{
		videos:    refreshTestPoller{name: "videos"},
		shorts:    refreshTestPoller{name: "shorts"},
		community: refreshTestPoller{name: "community"},
		stats:     refreshTestPoller{name: "channel_stats"},
		live:      refreshTestPoller{name: "live"},
	}
	if scraperConfig.PollTiering.Enabled {
		targets := Targets{NotificationChannelIDs: notificationChannelIDs, StatsChannelIDs: statsChannelIDs}
		if postgres != nil {
			if tiered, err := ClassifyByActivity(context.Background(), postgres.GetPool(), targets, time.Now()); err == nil {
				return buildTestTieredRegistrations(&pollers, poll, &tiered)
			}
		}
	}
	return buildTestFlatRegistrations(&pollers, poll, notificationChannelIDs, statsChannelIDs)
}

type testPollerSet struct {
	videos    scheduler.Poller
	shorts    scheduler.Poller
	community scheduler.Poller
	stats     scheduler.Poller
	live      scheduler.Poller
}

func buildTestFlatRegistrations(
	pollers *testPollerSet,
	poll settings.ScraperPoll,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	communityInterval := testCommunityPrimaryPollInterval(poll)
	return []providers.ChannelPollerRegistration{
		testNotificationRegistration(pollers.videos, scheduler.PriorityNormal, poll.Videos, notificationChannelIDs),
		testNotificationRegistration(pollers.shorts, scheduler.PriorityLow, poll.Shorts, notificationChannelIDs),
		testNotificationRegistration(pollers.community, scheduler.PriorityLow, communityInterval, notificationChannelIDs),
		providers.NewChannelPollerRegistration(pollers.stats, scheduler.PriorityLow, poll.Stats).
			WithChannelIDs(statsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats),
		testNotificationRegistration(pollers.live, scheduler.PriorityHigh, poll.Live, notificationChannelIDs),
	}
}

func buildTestTieredRegistrations(
	pollers *testPollerSet,
	poll settings.ScraperPoll,
	targets *TieredTargets,
) []providers.ChannelPollerRegistration {
	registrations := make([]providers.ChannelPollerRegistration, 0, 11)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.videos, poll.Videos, scheduler.PriorityNormal, targets)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.shorts, poll.Shorts, scheduler.PriorityLow, targets)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.community, testCommunityPrimaryPollInterval(poll), scheduler.PriorityLow, targets)
	registrations = append(registrations,
		providers.NewChannelPollerRegistration(pollers.stats, scheduler.PriorityLow, poll.Stats).
			WithChannelIDs(targets.StatsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats),
		testNotificationRegistration(pollers.live, scheduler.PriorityHigh, poll.Live, targets.NotificationChannelIDs),
	)
	return registrations
}

func testCommunityPrimaryPollInterval(poll settings.ScraperPoll) time.Duration {
	if poll.Shorts > 0 {
		return poll.Shorts
	}
	return poll.Community
}

func appendTestTieredNotificationRegistrations(
	registrations []providers.ChannelPollerRegistration,
	pollerInstance scheduler.Poller,
	baseInterval time.Duration,
	basePriority scheduler.Priority,
	targets *TieredTargets,
) []providers.ChannelPollerRegistration {
	warmPriority := scheduler.PriorityNormal
	if basePriority == scheduler.PriorityLow {
		warmPriority = scheduler.PriorityLow
	}
	registrations = append(registrations,
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupActive, basePriority, baseInterval, targets.ActiveNotificationChannelIDs),
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupWarm, warmPriority, baseInterval*2, targets.WarmNotificationChannelIDs),
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupCold, scheduler.PriorityLow, baseInterval*6, targets.ColdNotificationChannelIDs),
	)
	return registrations
}

func testNotificationRegistration(
	pollerInstance scheduler.Poller,
	priority scheduler.Priority,
	interval time.Duration,
	channelIDs []string,
) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(providers.ChannelTargetGroupNotification)
}

func testTieredNotificationRegistration(
	pollerInstance scheduler.Poller,
	targetGroup providers.ChannelTargetGroup,
	priority scheduler.Priority,
	interval time.Duration,
	channelIDs []string,
) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(targetGroup)
}
