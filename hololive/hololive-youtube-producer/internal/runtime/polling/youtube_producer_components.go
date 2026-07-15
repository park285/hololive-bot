package polling

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildYouTubeProducerComponents(
	ctx context.Context,
	scraperConfig *config.ScraperConfig,
	jobClaimer poller.JobClaimer,
	budgetWiring *GlobalBudgetWiring,
	postgresService database.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	logger *slog.Logger,
) (*poller.Scheduler, []providers.ChannelPollerRegistration, error) {
	if scraperConfig == nil {
		scraperConfig = &config.ScraperConfig{}
	}
	pollerRegistrations := buildYouTubeProducerChannelPollerRegistrationsWithClient(
		ctx,
		postgresService,
		scraperConfig,
		scraperClient,
		liveStatusProvider,
		notificationChannelIDs,
		statsChannelIDs,
	)
	if budgetWiring == nil {
		budgetWiring = &GlobalBudgetWiring{}
	}
	pollerRegistrations = wrapYouTubeProducerSourceCooldownPollers(pollerRegistrations, budgetWiring.Limiter, logger)
	limiterConfigured := scraperClient != nil && scraperClient.RateLimiterConfigured()
	if err := validateYouTubeProducerRegistrationsAndBudgets(pollerRegistrations, scraperConfig, budgetWiring, limiterConfigured, logger); err != nil {
		return nil, nil, err
	}

	schedulerOptions := buildYouTubeProducerSchedulerOptions(pollerRegistrations, scraperConfig, jobClaimer, budgetWiring)
	scraperScheduler := providers.ProvideScraperScheduler(nil, logger, schedulerOptions...)

	return scraperScheduler, pollerRegistrations, nil
}

func validateYouTubeProducerRegistrationsAndBudgets(
	pollerRegistrations []providers.ChannelPollerRegistration,
	scraperConfig *config.ScraperConfig,
	budgetWiring *GlobalBudgetWiring,
	limiterConfigured bool,
	logger *slog.Logger,
) error {
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return err
	}
	if err := validateRegistrationBudgetProfiles(pollerRegistrations); err != nil {
		return err
	}
	activeAPCount := resolveYouTubeProducerActiveAPCount(budgetWiring.ActiveInstanceCount, scraperConfig.ActiveActive.Enabled)
	sourceBudgetEstimate := estimateYouTubeProducerSourceBudget(
		pollerRegistrations,
		activeAPCount,
		scraperConfig.WorkerCountOrDefault(),
	)
	logYouTubeProducerSourceBudgetEstimate(sourceBudgetEstimate, logger)
	budgetSummary := summarizeYouTubeProducerBudgetForFleet(pollerRegistrations, budgetWiring.BudgetRPM, activeAPCount)
	logYouTubeProducerBudgetSummary(budgetSummary, logger)
	return validateYouTubeProducerRuntimeBudget(budgetSummary, limiterConfigured)
}

func buildYouTubeProducerSchedulerOptions(
	pollerRegistrations []providers.ChannelPollerRegistration,
	scraperConfig *config.ScraperConfig,
	jobClaimer poller.JobClaimer,
	budgetWiring *GlobalBudgetWiring,
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
	scraperConfig *config.ScraperConfig,
	cacheService cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	if scraperConfig == nil {
		scraperConfig = &config.ScraperConfig{}
	}
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
		opts = append(opts, scraper.WithChannelHealthPolicy(&scraper.ChannelHealthPolicy{
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
