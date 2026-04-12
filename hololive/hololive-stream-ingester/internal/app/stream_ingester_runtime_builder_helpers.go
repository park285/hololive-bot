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
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	cacheService cache.Client,
	irisClient iris.Sender,
	templateRenderer *template.Renderer,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration, error) {
	pollerRegistrations := buildStreamIngesterChannelPollerRegistrationsWithClient(
		postgresService,
		scraperCfg,
		scraperClient,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return nil, nil, nil, err
	}

	scraperScheduler := providers.ProvideScraperScheduler(
		nil,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		delivery.NewIrisMessageSender(irisClient),
		templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher, pollerRegistrations, nil
}

func buildSharedYouTubeScraperClient(
	scraperCfg config.ScraperConfig,
	cacheService cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	proxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}

	return scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheService),
	)
}

func buildPendingPublishedAtResolver(
	postgresService database.Client,
	scraperClient *scraper.Client,
	cacheService cache.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	if postgresService == nil || scraperClient == nil {
		return nil
	}

	softLimiter := scraper.NewRateLimiter(15 * time.Second)
	backoffStore := poller.NewCachePublishedAtBackoffStore(cacheService)

	return poller.NewPendingPublishedAtResolverWithControls(
		postgresService.GetGormDB(),
		scraperClient,
		routeDecider,
		15*time.Second,
		50,
		20,
		20*time.Second,
		5*time.Minute,
		softLimiter,
		backoffStore,
		logger,
	)
}

func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	missing := make([]string, 0)
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if registration.HasExplicitChannelIDs {
			continue
		}
		missing = append(missing, registration.Poller.Name())
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"stream-ingester poller registrations require explicit channel IDs: %s",
		strings.Join(missing, ", "),
	)
}
