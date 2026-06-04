package publishedat

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildPendingPublishedAtResolver(
	scraperConfig config.ScraperConfig,
	postgresService database.Client,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	if postgresService == nil || scraperClient == nil {
		return nil
	}
	resolverConfig := effectivePublishedAtResolverConfig(scraperConfig)
	if !resolverConfig.Enabled {
		return nil
	}
	if routeDecider == nil {
		if logger != nil {
			logger.Info("published_at_resolver_inactive_without_route_decider")
		}
		return nil
	}

	resolver := poller.NewPendingPublishedAtResolverWithControls(
		postgresService.GetPool(),
		scraperClient,
		routeDecider,
		resolverConfig.Interval,
		resolverConfig.BatchSize,
		resolverConfig.MaxResolvePerRun,
		resolverConfig.MaxRunDuration,
		resolverConfig.ResolveTimeout,
		resolverConfig.MinDetectedAge,
		resolverConfig.FailureBackoffTTL,
		logger,
	)
	if logger != nil {
		logger.Info("published_at_resolver_configured",
			slog.Duration("interval", resolverConfig.Interval),
			slog.Int("batch_size", resolverConfig.BatchSize),
			slog.Int("max_resolve_per_run", resolverConfig.MaxResolvePerRun),
			slog.Duration("max_run_duration", resolverConfig.MaxRunDuration),
			slog.Duration("resolve_timeout", resolverConfig.ResolveTimeout),
			slog.Duration("min_detected_age", resolverConfig.MinDetectedAge),
			slog.Duration("failure_backoff_ttl", resolverConfig.FailureBackoffTTL),
			slog.Float64("estimated_max_rpm", estimatedPublishedAtResolverMaxRPM(resolverConfig)),
		)
	}
	return resolver
}

func buildPublishedAtResolverRegistration(
	resolver *poller.PendingPublishedAtResolver,
	scraperConfig config.ScraperConfig,
	logger *slog.Logger,
) *providers.ChannelPollerRegistration {
	if resolver == nil {
		return nil
	}

	resolverPoller := poller.NewPendingPublishedAtResolverPoller(resolver)
	if resolverPoller == nil {
		return nil
	}

	resolverConfig := effectivePublishedAtResolverConfig(scraperConfig)
	registration := providers.NewGlobalPollerRegistration(
		resolverPoller,
		poller.PriorityLow,
		resolverConfig.Interval,
	).WithRequestsPerRun(resolverConfig.MaxResolvePerRun).
		WithWorstCaseAttempts(scraper.MetadataResolveFetchPolicy.MaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(resolverConfig.MaxResolvePerRun * scraper.MetadataResolveFetchPolicy.MaxAttempts)).
		WithBudgetProfile(poller.BudgetProfile{
			SourceUnits: map[poller.BudgetSource]float64{
				poller.BudgetSourceYouTubeScraper: float64(resolverConfig.MaxResolvePerRun * scraper.MetadataResolveFetchPolicy.MaxAttempts),
				poller.BudgetSourcePostgresWrite:  1,
			},
			BurstClass: poller.BudgetBurstPrimary,
			Priority:   poller.BudgetPriorityLow,
		})
	if logger != nil {
		logger.Info("published_at_resolver_registered_with_scraper_scheduler",
			slog.Duration("interval", resolverConfig.Interval),
			slog.String("target", providers.SyntheticGlobalPollerChannelID),
			slog.Int("requests_per_run", resolverConfig.MaxResolvePerRun),
			slog.Int("worst_case_attempts", scraper.MetadataResolveFetchPolicy.MaxAttempts),
		)
	}
	return &registration
}

func effectivePublishedAtResolverConfig(scraperConfig config.ScraperConfig) config.ScraperPublishedAtResolverConfig {
	resolverConfig := scraperConfig.PublishedAtResolver
	if !resolverConfig.Enabled {
		return resolverConfig
	}

	defaults := config.DefaultScraperPublishedAtResolverConfig()
	if resolverConfig.Interval <= 0 {
		resolverConfig.Interval = defaults.Interval
	}
	if resolverConfig.BatchSize <= 0 {
		resolverConfig.BatchSize = defaults.BatchSize
	}
	if resolverConfig.MaxResolvePerRun <= 0 {
		resolverConfig.MaxResolvePerRun = defaults.MaxResolvePerRun
	}
	if resolverConfig.MaxRunDuration <= 0 {
		resolverConfig.MaxRunDuration = defaults.MaxRunDuration
	}
	if resolverConfig.ResolveTimeout <= 0 {
		resolverConfig.ResolveTimeout = defaults.ResolveTimeout
	}
	if resolverConfig.MinDetectedAge <= 0 {
		resolverConfig.MinDetectedAge = defaults.MinDetectedAge
	}
	if resolverConfig.FailureBackoffTTL <= 0 {
		resolverConfig.FailureBackoffTTL = defaults.FailureBackoffTTL
	}
	return resolverConfig
}

func estimatedPublishedAtResolverMaxRPM(resolverConfig config.ScraperPublishedAtResolverConfig) float64 {
	if resolverConfig.Interval <= 0 || resolverConfig.MaxResolvePerRun <= 0 {
		return 0
	}
	return float64(resolverConfig.MaxResolvePerRun) * 60 / resolverConfig.Interval.Seconds()
}
