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

package app

import (
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildStreamIngesterChannelPollerRegistrations(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc cache.Client,
	routeDecider poller.NotificationRouteDecider,
) []providers.ChannelPollerRegistration {
	proxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}
	poll := scraperCfg.PollOrDefault()
	communityKeywords := []string{}

	scraperClient := scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheSvc),
	)
	db := postgres.GetGormDB()

	videosPoller := poller.NewVideosPoller(scraperClient, db, 10)
	shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider)
	communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider)
	statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
	livePoller := poller.NewLivePoller(scraperClient, db)

	return []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(videosPoller, poller.PriorityNormal, poll.Videos),
		providers.NewChannelPollerRegistration(shortsPoller, poller.PriorityLow, poll.Shorts),
		providers.NewChannelPollerRegistration(communityPoller, poller.PriorityLow, poll.Community),
		providers.NewChannelPollerRegistration(statsPoller, poller.PriorityLow, poll.Stats),
		providers.NewChannelPollerRegistration(livePoller, poller.PriorityHigh, poll.Live),
	}
}
