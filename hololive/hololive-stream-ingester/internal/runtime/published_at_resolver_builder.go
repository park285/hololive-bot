package runtime

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildPendingPublishedAtResolver(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	if postgresService == nil || scraperClient == nil {
		return nil
	}
	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	if !resolverCfg.Enabled {
		return nil
	}
	if routeDecider == nil {
		if logger != nil {
			logger.Info("published_at_resolver_inactive_without_route_decider")
		}
		return nil
	}

	resolver := poller.NewPendingPublishedAtResolverWithControls(
		postgresService.GetGormDB(),
		scraperClient,
		routeDecider,
		resolverCfg.Interval,
		resolverCfg.BatchSize,
		resolverCfg.MaxResolvePerRun,
		resolverCfg.MaxRunDuration,
		resolverCfg.ResolveTimeout,
		resolverCfg.MinDetectedAge,
		resolverCfg.FailureBackoffTTL,
		logger,
	)
	if logger != nil {
		logger.Info("published_at_resolver_configured",
			slog.Duration("interval", resolverCfg.Interval),
			slog.Int("batch_size", resolverCfg.BatchSize),
			slog.Int("max_resolve_per_run", resolverCfg.MaxResolvePerRun),
			slog.Duration("max_run_duration", resolverCfg.MaxRunDuration),
			slog.Duration("resolve_timeout", resolverCfg.ResolveTimeout),
			slog.Duration("min_detected_age", resolverCfg.MinDetectedAge),
			slog.Duration("failure_backoff_ttl", resolverCfg.FailureBackoffTTL),
			slog.Float64("estimated_max_rpm", estimatedPublishedAtResolverMaxRPM(resolverCfg)),
		)
	}
	return resolver
}

func activePublishedAtResolverBudgetConfig(
	scraperCfg config.ScraperConfig,
	resolver *poller.PendingPublishedAtResolver,
) *config.ScraperPublishedAtResolverConfig {
	if resolver == nil {
		return nil
	}
	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	return &resolverCfg
}

func registerPublishedAtResolverPoller(
	scheduler *poller.Scheduler,
	resolver *poller.PendingPublishedAtResolver,
	scraperCfg config.ScraperConfig,
	logger *slog.Logger,
) {
	if scheduler == nil || resolver == nil {
		return
	}

	resolverPoller := poller.NewPendingPublishedAtResolverPoller(resolver)
	if resolverPoller == nil {
		return
	}

	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	scheduler.Register(
		providers.SyntheticGlobalPollerChannelID,
		resolverPoller,
		poller.PriorityLow,
		resolverCfg.Interval,
	)
	if logger != nil {
		logger.Info("published_at_resolver_registered_with_scraper_scheduler",
			slog.Duration("interval", resolverCfg.Interval),
			slog.String("target", providers.SyntheticGlobalPollerChannelID),
		)
	}
}

func effectivePublishedAtResolverConfig(scraperCfg config.ScraperConfig) config.ScraperPublishedAtResolverConfig {
	resolverCfg := scraperCfg.PublishedAtResolver
	if !resolverCfg.Enabled {
		return resolverCfg
	}

	defaults := config.DefaultScraperPublishedAtResolverConfig()
	if resolverCfg.Interval <= 0 {
		resolverCfg.Interval = defaults.Interval
	}
	if resolverCfg.BatchSize <= 0 {
		resolverCfg.BatchSize = defaults.BatchSize
	}
	if resolverCfg.MaxResolvePerRun <= 0 {
		resolverCfg.MaxResolvePerRun = defaults.MaxResolvePerRun
	}
	if resolverCfg.MaxRunDuration <= 0 {
		resolverCfg.MaxRunDuration = defaults.MaxRunDuration
	}
	if resolverCfg.ResolveTimeout <= 0 {
		resolverCfg.ResolveTimeout = defaults.ResolveTimeout
	}
	if resolverCfg.MinDetectedAge <= 0 {
		resolverCfg.MinDetectedAge = defaults.MinDetectedAge
	}
	if resolverCfg.FailureBackoffTTL <= 0 {
		resolverCfg.FailureBackoffTTL = defaults.FailureBackoffTTL
	}
	return resolverCfg
}

func estimatedPublishedAtResolverMaxRPM(cfg config.ScraperPublishedAtResolverConfig) float64 {
	if cfg.Interval <= 0 || cfg.MaxResolvePerRun <= 0 {
		return 0
	}
	return float64(cfg.MaxResolvePerRun) * 60 / cfg.Interval.Seconds()
}

func estimatedPublishedAtResolverWorstCaseRPM(cfg config.ScraperPublishedAtResolverConfig) float64 {
	return estimatedPublishedAtResolverMaxRPM(cfg) * float64(scraper.FetchPageMaxAttempts)
}
