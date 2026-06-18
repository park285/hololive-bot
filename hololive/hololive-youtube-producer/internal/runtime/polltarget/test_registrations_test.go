package polltarget

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildYouTubeProducerChannelPollerRegistrations(
	postgres database.Client,
	scraperConfig *config.ScraperConfig,
	_ *scraper.RateLimiter,
	_ cache.Client,
	_ poller.NotificationRouteDecider,
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
	videos    poller.Poller
	shorts    poller.Poller
	community poller.Poller
	stats     poller.Poller
	live      poller.Poller
}

func buildTestFlatRegistrations(
	pollers *testPollerSet,
	poll config.ScraperPoll,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	communityInterval := testCommunityPrimaryPollInterval(poll)
	return []providers.ChannelPollerRegistration{
		testNotificationRegistration(pollers.videos, poller.PriorityNormal, poll.Videos, notificationChannelIDs),
		testNotificationRegistration(pollers.shorts, poller.PriorityLow, poll.Shorts, notificationChannelIDs),
		testNotificationRegistration(pollers.community, poller.PriorityLow, communityInterval, notificationChannelIDs),
		providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
			WithChannelIDs(statsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats),
		testNotificationRegistration(pollers.live, poller.PriorityHigh, poll.Live, notificationChannelIDs),
	}
}

func buildTestTieredRegistrations(
	pollers *testPollerSet,
	poll config.ScraperPoll,
	targets *TieredTargets,
) []providers.ChannelPollerRegistration {
	registrations := make([]providers.ChannelPollerRegistration, 0, 11)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.videos, poll.Videos, poller.PriorityNormal, targets)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.shorts, poll.Shorts, poller.PriorityLow, targets)
	registrations = appendTestTieredNotificationRegistrations(registrations, pollers.community, testCommunityPrimaryPollInterval(poll), poller.PriorityLow, targets)
	registrations = append(registrations,
		providers.NewChannelPollerRegistration(pollers.stats, poller.PriorityLow, poll.Stats).
			WithChannelIDs(targets.StatsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats),
		testNotificationRegistration(pollers.live, poller.PriorityHigh, poll.Live, targets.NotificationChannelIDs),
	)
	return registrations
}

func testCommunityPrimaryPollInterval(poll config.ScraperPoll) time.Duration {
	if poll.Shorts > 0 {
		return poll.Shorts
	}
	return poll.Community
}

func appendTestTieredNotificationRegistrations(
	registrations []providers.ChannelPollerRegistration,
	pollerInstance poller.Poller,
	baseInterval time.Duration,
	basePriority poller.Priority,
	targets *TieredTargets,
) []providers.ChannelPollerRegistration {
	warmPriority := poller.PriorityNormal
	if basePriority == poller.PriorityLow {
		warmPriority = poller.PriorityLow
	}
	registrations = append(registrations,
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupActive, basePriority, baseInterval, targets.ActiveNotificationChannelIDs),
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupWarm, warmPriority, baseInterval*2, targets.WarmNotificationChannelIDs),
		testTieredNotificationRegistration(pollerInstance, providers.ChannelTargetGroupCold, poller.PriorityLow, baseInterval*6, targets.ColdNotificationChannelIDs),
	)
	return registrations
}

func testNotificationRegistration(
	pollerInstance poller.Poller,
	priority poller.Priority,
	interval time.Duration,
	channelIDs []string,
) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(providers.ChannelTargetGroupNotification)
}

func testTieredNotificationRegistration(
	pollerInstance poller.Poller,
	targetGroup providers.ChannelTargetGroup,
	priority poller.Priority,
	interval time.Duration,
	channelIDs []string,
) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(pollerInstance, priority, interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(targetGroup)
}
