package polling

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type BudgetSummary = youtubeProducerBudgetSummary

func BuildComponents(
	scraperConfig config.ScraperConfig,
	postgresService database.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	publishedAtResolver *poller.PendingPublishedAtResolver,
	logger *slog.Logger,
) (*poller.Scheduler, []providers.ChannelPollerRegistration, error) {
	return BuildComponentsWithJobClaimer(
		scraperConfig,
		nil,
		postgresService,
		notificationChannelIDs,
		statsChannelIDs,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		publishedAtResolver,
		logger,
	)
}

func BuildComponentsWithJobClaimer(
	scraperConfig config.ScraperConfig,
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
	return buildYouTubeProducerComponents(
		scraperConfig,
		jobClaimer,
		postgresService,
		notificationChannelIDs,
		statsChannelIDs,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		publishedAtResolver,
		logger,
	)
}

func BuildSharedClient(
	scraperConfig config.ScraperConfig,
	cacheClient cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	return buildSharedYouTubeProducerClient(scraperConfig, cacheClient, sharedRL)
}

func BuildRegistrations(
	postgres database.Client,
	scraperConfig config.ScraperConfig,
	sharedRL *scraper.RateLimiter,
	cacheClient cache.Client,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildYouTubeProducerChannelPollerRegistrations(
		postgres,
		scraperConfig,
		sharedRL,
		cacheClient,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func BuildRegistrationsWithClient(
	postgres database.Client,
	scraperConfig config.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		postgres,
		scraperConfig,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func SummarizeBudget(registrations []providers.ChannelPollerRegistration) BudgetSummary {
	return summarizeYouTubeProducerBudget(registrations)
}

func LogBudgetSummary(summary BudgetSummary, logger *slog.Logger) {
	logYouTubeProducerBudgetSummary(summary, logger)
}

func EstimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
	return estimateResolvedPollerRPM(registrations)
}

func ValidateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	return validateExplicitPollerRegistrations(registrations)
}
