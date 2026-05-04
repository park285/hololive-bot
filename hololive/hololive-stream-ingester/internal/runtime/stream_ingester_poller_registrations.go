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

package runtime

import (
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
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
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func buildStreamIngesterChannelPollerRegistrationsWithClient(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	poll := scraperCfg.PollOrDefault()
	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	inlineResolveMissingPublishedAt := routeDecider != nil && !resolverCfg.Enabled
	communityKeywords := []string{}
	db := postgres.GetGormDB()
	maxResults := defaultChannelPollerMaxResults

	videosPoller := poller.NewVideosPoller(scraperClient, db, maxResults)
	shortsPoller := poller.NewShortsPoller(scraperClient, db, maxResults, routeDecider, inlineResolveMissingPublishedAt)
	communityPoller := poller.NewCommunityPoller(scraperClient, db, maxResults, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)
	statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
	livePoller := poller.NewLivePoller(scraperClient, db)

	return []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(videosPoller, poller.PriorityNormal, poll.Videos).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(videosWorstCaseRequestUnits()),
		providers.NewChannelPollerRegistration(shortsPoller, poller.PriorityLow, poll.Shorts).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)),
		providers.NewChannelPollerRegistration(communityPoller, poller.PriorityLow, poll.Community).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts).
			WithWorstCaseRequestUnitsPerRun(communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt, maxResults)),
		providers.NewChannelPollerRegistration(statsPoller, poller.PriorityLow, poll.Stats).
			WithChannelIDs(statsChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupStats).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),
		providers.NewChannelPollerRegistration(livePoller, poller.PriorityHigh, poll.Live).
			WithChannelIDs(notificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),
	}
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
