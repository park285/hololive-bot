package polling

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/pollers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type GlobalBudgetWiring struct {
	Limiter             polling.GlobalBudgetLimiter
	Context             polling.BudgetContext
	AcquireTimeout      time.Duration
	ActiveInstanceCount int
	BudgetRPM           float64
}

func BuildComponents(
	scraperConfig *settings.ScraperConfig,
	postgresService database.Client,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	logger *slog.Logger,
) (*scheduler.Scheduler, []providers.ChannelPollerRegistration, error) {
	return BuildComponentsWithJobClaimerContext(
		context.Background(),
		scraperConfig,
		nil,
		&GlobalBudgetWiring{},
		postgresService,
		notificationChannelIDs,
		operationalChannelIDs,
		scraperClient,
		liveStatusProvider,
		logger,
	)
}

func BuildComponentsWithJobClaimer(
	scraperConfig *settings.ScraperConfig,
	jobClaimer polling.JobClaimer,
	budgetWiring *GlobalBudgetWiring,
	postgresService database.Client,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	logger *slog.Logger,
) (*scheduler.Scheduler, []providers.ChannelPollerRegistration, error) {
	return BuildComponentsWithJobClaimerContext(
		context.Background(),
		scraperConfig,
		jobClaimer,
		budgetWiring,
		postgresService,
		notificationChannelIDs,
		operationalChannelIDs,
		scraperClient,
		liveStatusProvider,
		logger,
	)
}

func BuildComponentsWithJobClaimerContext(
	ctx context.Context,
	scraperConfig *settings.ScraperConfig,
	jobClaimer polling.JobClaimer,
	budgetWiring *GlobalBudgetWiring,
	postgresService database.Client,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
	scraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	logger *slog.Logger,
) (*scheduler.Scheduler, []providers.ChannelPollerRegistration, error) {
	return buildYouTubeProducerComponents(
		ctx,
		scraperConfig,
		jobClaimer,
		budgetWiring,
		postgresService,
		notificationChannelIDs,
		operationalChannelIDs,
		scraperClient,
		liveStatusProvider,
		logger,
	)
}

func BuildSharedClient(
	scraperConfig *settings.ScraperConfig,
	cacheClient cache.Client,
	sharedRL *ratelimiter.RateLimiter,
) *scraper.Client {
	return buildSharedYouTubeProducerClient(scraperConfig, cacheClient, sharedRL)
}

func BuildRegistrations(
	postgres database.Client,
	scraperConfig *settings.ScraperConfig,
	sharedRL *ratelimiter.RateLimiter,
	cacheClient cache.Client,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildYouTubeProducerChannelPollerRegistrations(
		context.Background(),
		postgres,
		scraperConfig,
		sharedRL,
		cacheClient,
		notificationChannelIDs,
		operationalChannelIDs,
	)
}

func BuildRegistrationsWithClient(
	postgres database.Client,
	scraperConfig *settings.ScraperConfig,
	scraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	notificationChannelIDs []string,
	operationalChannelIDs []string,
) []providers.ChannelPollerRegistration {
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		context.Background(),
		postgres,
		scraperConfig,
		scraperClient,
		liveStatusProvider,
		notificationChannelIDs,
		operationalChannelIDs,
		slog.Default(),
	)
}

func SummarizeBudget(registrations []providers.ChannelPollerRegistration) youtubeProducerBudgetSummary {
	return summarizeYouTubeProducerBudget(registrations)
}

func LogBudgetSummary(summary youtubeProducerBudgetSummary, logger *slog.Logger) {
	logYouTubeProducerBudgetSummary(summary, logger)
}

func EstimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
	return estimateResolvedPollerRPM(registrations)
}

func ValidateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	return validateExplicitPollerRegistrations(registrations)
}
