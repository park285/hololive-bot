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
	scraperConfig config.ScraperConfig,
	jobClaimer poller.JobClaimer,
	budgetWiring GlobalBudgetWiring,
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
		scraperConfig,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
	if resolverRegistration := publishedat.BuildRegistration(publishedAtResolver, scraperConfig, logger); resolverRegistration != nil {
		pollerRegistrations = append(pollerRegistrations, *resolverRegistration)
	}
	pollerRegistrations = wrapYouTubeProducerSourceCooldownPollers(pollerRegistrations, budgetWiring.Limiter, logger)
	if err := validateYouTubeProducerRegistrationsAndBudgets(pollerRegistrations, scraperConfig, budgetWiring, logger); err != nil {
		return nil, nil, err
	}

	schedulerOptions := buildYouTubeProducerSchedulerOptions(pollerRegistrations, scraperConfig, jobClaimer, budgetWiring)
	scraperScheduler := providers.ProvideScraperScheduler(nil, logger, schedulerOptions...)

	return scraperScheduler, pollerRegistrations, nil
}

func validateYouTubeProducerRegistrationsAndBudgets(
	pollerRegistrations []providers.ChannelPollerRegistration,
	scraperConfig config.ScraperConfig,
	budgetWiring GlobalBudgetWiring,
	logger *slog.Logger,
) error {
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return err
	}
	if err := validateRegistrationBudgetProfiles(pollerRegistrations); err != nil {
		return err
	}
	sourceBudgetEstimate := estimateYouTubeProducerSourceBudget(
		pollerRegistrations,
		resolveYouTubeProducerActiveAPCount(budgetWiring.ActiveInstanceCount, scraperConfig.ActiveActive.Enabled),
		scraperConfig.WorkerCountOrDefault(),
	)
	logYouTubeProducerSourceBudgetEstimate(sourceBudgetEstimate, logger)
	budgetSummary := summarizeYouTubeProducerBudget(pollerRegistrations)
	logYouTubeProducerBudgetSummary(budgetSummary, logger)
	return validateYouTubeProducerPollerBudget(budgetSummary)
}

func buildYouTubeProducerSchedulerOptions(
	pollerRegistrations []providers.ChannelPollerRegistration,
	scraperConfig config.ScraperConfig,
	jobClaimer poller.JobClaimer,
	budgetWiring GlobalBudgetWiring,
) []providers.ScraperSchedulerOption {
	schedulerConfig := scraperConfig.SchedulerOrDefault()
	schedulerOptions := []providers.ScraperSchedulerOption{
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperConfig.WorkerCountOrDefault()),
		providers.WithSchedulerPollTimeout(schedulerConfig.PollTimeout),
		providers.WithSchedulerErrorBackoff(schedulerConfig.ErrorBackoffMin, schedulerConfig.ErrorBackoffMax),
		providers.WithSchedulerJobClaimer(jobClaimer),
	}
	if budgetWiring.Limiter != nil {
		schedulerOptions = append(
			schedulerOptions,
			providers.WithSchedulerBudgetLimiter(budgetWiring.Limiter),
			providers.WithSchedulerBudgetContext(budgetWiring.Context),
		)
	}
	if budgetWiring.AcquireTimeout > 0 {
		schedulerOptions = append(schedulerOptions, providers.WithSchedulerBudgetAcquireTimeout(budgetWiring.AcquireTimeout))
	}
	return schedulerOptions
}

func buildSharedYouTubeProducerClient(
	scraperConfig config.ScraperConfig,
	cacheService cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	proxyConfig := scraper.ProxyConfig{
		Enabled: scraperConfig.ProxyEnabled,
		URL:     scraperConfig.ProxyURL,
	}
	snapshotConfig := scraperConfig.SnapshotOrDefault()
	channelHealthConfig := scraperConfig.ChannelHealthOrDefault()
	browserConfig := scraperConfig.BrowserDiagnosticOrDefault()

	opts := []scraper.ClientOption{
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheService),
		scraper.WithFetcherEngine(scraper.FetcherEngine(scraperConfig.FetcherEngine)),
		scraper.WithSnapshotPolicy(scraper.SnapshotPolicy{
			Enabled:      snapshotConfig.Enabled,
			MaxBodyBytes: snapshotConfig.MaxBodyBytes,
			MinInterval:  snapshotConfig.MinInterval,
			AllowedReasons: map[scraper.FailureReason]bool{
				scraper.FailureReasonParserDrift:      true,
				scraper.FailureReasonEmptyResponse:    true,
				scraper.FailureReasonBlockedResponse:  true,
				scraper.FailureReasonResponseTooLarge: true,
			},
		}),
	}
	if channelHealthConfig.Enabled {
		opts = append(opts, scraper.WithChannelHealthPolicy(scraper.ChannelHealthPolicy{
			Enforce:           channelHealthConfig.Enforce,
			TTL:               channelHealthConfig.TTL,
			ParserDriftBase:   channelHealthConfig.ParserDriftBase,
			ParserDriftMax:    channelHealthConfig.ParserDriftMax,
			TransportBase:     channelHealthConfig.TransportBase,
			TransportMax:      channelHealthConfig.TransportMax,
			TimeoutBase:       channelHealthConfig.TimeoutBase,
			TimeoutMax:        channelHealthConfig.TimeoutMax,
			HTTPStatusBase:    channelHealthConfig.HTTPStatusBase,
			HTTPStatusMax:     channelHealthConfig.HTTPStatusMax,
			SuccessDecaySteps: channelHealthConfig.SuccessDecaySteps,
		}))
	} else {
		opts = append(opts, scraper.WithChannelHealthDisabled())
	}
	if snapshotConfig.Enabled {
		opts = append(opts, scraper.WithSnapshotSink(scraper.NewFileSnapshotSink(snapshotConfig.Dir)))
	}
	if browserConfig.Enabled && browserConfig.Endpoint != "" {
		opts = append(opts, scraper.WithBrowserSnapshotFetcher(scraper.NewBrowserSnapshotFetcher(browserConfig.Endpoint, browserConfig.Timeout)))
	}

	return scraper.NewClient(opts...)
}
