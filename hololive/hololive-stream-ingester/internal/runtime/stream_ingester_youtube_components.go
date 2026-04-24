package runtime

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
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	cacheService cache.Client,
	irisClient iris.Sender,
	templateRenderer *template.Renderer,
	routeDecider poller.NotificationRouteDecider,
	publishedAtResolver *poller.PendingPublishedAtResolver,
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
	if resolverRegistration := buildPublishedAtResolverRegistration(publishedAtResolver, scraperCfg, logger); resolverRegistration != nil {
		pollerRegistrations = append(pollerRegistrations, *resolverRegistration)
	}
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return nil, nil, nil, err
	}
	budgetSummary := summarizeYouTubeScraperBudget(pollerRegistrations)
	logYouTubeScraperBudgetSummary(budgetSummary, logger)
	if err := validateYouTubeScraperPollerBudget(budgetSummary); err != nil {
		return nil, nil, nil, err
	}

	schedulerCfg := scraperCfg.SchedulerOrDefault()
	scraperScheduler := providers.ProvideScraperScheduler(
		nil,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
		providers.WithSchedulerPollTimeout(schedulerCfg.PollTimeout),
		providers.WithSchedulerErrorBackoff(schedulerCfg.ErrorBackoffMin, schedulerCfg.ErrorBackoffMax),
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
