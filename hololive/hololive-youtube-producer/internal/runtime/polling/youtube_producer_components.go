package polling

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
)

func buildYouTubeProducerComponents(
	scraperCfg config.ScraperConfig,
	jobClaimer poller.JobClaimer,
	postgresService database.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	publishedAtResolver *poller.PendingPublishedAtResolver,
	logger *slog.Logger,
) (*poller.Scheduler, []providers.ChannelPollerRegistration, error) {
	pollerRegistrations := buildYouTubeProducerChannelPollerRegistrationsWithClient(
		postgresService,
		scraperCfg,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
	if resolverRegistration := publishedat.BuildRegistration(publishedAtResolver, scraperCfg, logger); resolverRegistration != nil {
		pollerRegistrations = append(pollerRegistrations, *resolverRegistration)
	}
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return nil, nil, err
	}
	budgetSummary := summarizeYouTubeProducerBudget(pollerRegistrations)
	logYouTubeProducerBudgetSummary(budgetSummary, logger)
	if err := validateYouTubeProducerPollerBudget(budgetSummary); err != nil {
		return nil, nil, err
	}

	schedulerCfg := scraperCfg.SchedulerOrDefault()
	scraperScheduler := providers.ProvideScraperScheduler(
		nil,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
		providers.WithSchedulerPollTimeout(schedulerCfg.PollTimeout),
		providers.WithSchedulerErrorBackoff(schedulerCfg.ErrorBackoffMin, schedulerCfg.ErrorBackoffMax),
		providers.WithSchedulerJobClaimer(jobClaimer),
	)

	return scraperScheduler, pollerRegistrations, nil
}

func buildSharedYouTubeProducerClient(
	scraperCfg config.ScraperConfig,
	cacheService cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	proxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}
	snapshotCfg := scraperCfg.SnapshotOrDefault()
	channelHealthCfg := scraperCfg.ChannelHealthOrDefault()
	browserCfg := scraperCfg.BrowserDiagnosticOrDefault()

	opts := []scraper.ClientOption{
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheService),
		scraper.WithFetcherEngine(scraper.FetcherEngine(scraperCfg.FetcherEngine)),
		scraper.WithSnapshotPolicy(scraper.SnapshotPolicy{
			Enabled:      snapshotCfg.Enabled,
			MaxBodyBytes: snapshotCfg.MaxBodyBytes,
			MinInterval:  snapshotCfg.MinInterval,
			AllowedReasons: map[scraper.FailureReason]bool{
				scraper.FailureReasonParserDrift:   true,
				scraper.FailureReasonEmptyResponse: true,
			},
		}),
	}
	if channelHealthCfg.Enabled {
		opts = append(opts, scraper.WithChannelHealthPolicy(scraper.ChannelHealthPolicy{
			Enforce:           channelHealthCfg.Enforce,
			TTL:               channelHealthCfg.TTL,
			ParserDriftBase:   channelHealthCfg.ParserDriftBase,
			ParserDriftMax:    channelHealthCfg.ParserDriftMax,
			TransportBase:     channelHealthCfg.TransportBase,
			TransportMax:      channelHealthCfg.TransportMax,
			TimeoutBase:       channelHealthCfg.TimeoutBase,
			TimeoutMax:        channelHealthCfg.TimeoutMax,
			HTTPStatusBase:    channelHealthCfg.HTTPStatusBase,
			HTTPStatusMax:     channelHealthCfg.HTTPStatusMax,
			SuccessDecaySteps: channelHealthCfg.SuccessDecaySteps,
		}))
	} else {
		opts = append(opts, scraper.WithChannelHealthDisabled())
	}
	if snapshotCfg.Enabled {
		opts = append(opts, scraper.WithSnapshotSink(scraper.NewFileSnapshotSink(snapshotCfg.Dir)))
	}
	if browserCfg.Enabled && browserCfg.Endpoint != "" {
		opts = append(opts, scraper.WithBrowserSnapshotFetcher(scraper.NewBrowserSnapshotFetcher(browserCfg.Endpoint, browserCfg.Timeout)))
	}

	return scraper.NewClient(opts...)
}
