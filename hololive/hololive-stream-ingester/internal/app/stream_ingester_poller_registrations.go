package app

import (
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildStreamIngesterChannelPollerRegistrations(
	postgres database.Client,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc cache.Client,
) []providers.ChannelPollerRegistration {
	intervals := providers.DefaultPollerIntervals()
	communityKeywords := []string{}

	scraperClient := scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheSvc),
	)
	db := postgres.GetGormDB()

	videosPoller := poller.NewVideosPoller(scraperClient, db, 10)
	shortsPoller := poller.NewShortsPoller(scraperClient, db, 10)
	communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords)
	statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
	livePoller := poller.NewLivePoller(scraperClient, db)

	return []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(videosPoller, poller.PriorityNormal, intervals.Videos),
		providers.NewChannelPollerRegistration(shortsPoller, poller.PriorityLow, intervals.Shorts),
		providers.NewChannelPollerRegistration(communityPoller, poller.PriorityLow, intervals.Community),
		providers.NewChannelPollerRegistration(statsPoller, poller.PriorityLow, intervals.Stats),
		providers.NewChannelPollerRegistration(livePoller, poller.PriorityHigh, intervals.Live),
	}
}
