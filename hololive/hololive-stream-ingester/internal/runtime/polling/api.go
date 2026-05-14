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

type BudgetSummary = youtubeScraperBudgetSummary

func BuildComponents(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	publishedAtResolver *poller.PendingPublishedAtResolver,
	logger *slog.Logger,
) (*poller.Scheduler, []providers.ChannelPollerRegistration, error) {
	return buildStreamIngesterYouTubeComponents(
		scraperCfg,
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
	scraperCfg config.ScraperConfig,
	cacheSvc cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	return buildSharedYouTubeScraperClient(scraperCfg, cacheSvc, sharedRL)
}

func BuildRegistrations(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc cache.Client,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildStreamIngesterChannelPollerRegistrations(
		postgres,
		scraperCfg,
		sharedRL,
		cacheSvc,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func BuildRegistrationsWithClient(
	postgres database.Client,
	scraperCfg config.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider poller.LiveStatusProvider,
	routeDecider poller.NotificationRouteDecider,
	notificationChannelIDs []string,
	statsChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildStreamIngesterChannelPollerRegistrationsWithClient(
		postgres,
		scraperCfg,
		scraperClient,
		liveStatusProvider,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
}

func SummarizeBudget(registrations []providers.ChannelPollerRegistration) BudgetSummary {
	return summarizeYouTubeScraperBudget(registrations)
}

func LogBudgetSummary(summary BudgetSummary, logger *slog.Logger) {
	logYouTubeScraperBudgetSummary(summary, logger)
}

func EstimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
	return estimateResolvedPollerRPM(registrations)
}

func ValidateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	return validateExplicitPollerRegistrations(registrations)
}
