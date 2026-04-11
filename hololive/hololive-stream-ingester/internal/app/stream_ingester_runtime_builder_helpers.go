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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/iris-client-go/iris"
)

func buildStreamIngesterYouTubeComponents(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	channelIDs []string,
	cacheService cache.Client,
	irisClient iris.Sender,
	templateRenderer *template.Renderer,
	sharedRL *scraper.RateLimiter,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher) {
	pollerRegistrations := buildStreamIngesterChannelPollerRegistrations(postgresService, scraperCfg, sharedRL, cacheService, routeDecider)

	scraperScheduler := providers.ProvideScraperScheduler(
		nil,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
		providers.WithSchedulerChannelIDs(channelIDs),
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		delivery.NewIrisMessageSender(irisClient),
		templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}
