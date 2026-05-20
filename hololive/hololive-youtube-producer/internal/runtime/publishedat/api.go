package publishedat

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func BuildPendingResolver(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	return buildPendingPublishedAtResolver(scraperCfg, postgresService, scraperClient, routeDecider, logger)
}

func BuildRegistration(
	resolver *poller.PendingPublishedAtResolver,
	scraperCfg config.ScraperConfig,
	logger *slog.Logger,
) *providers.ChannelPollerRegistration {
	return buildPublishedAtResolverRegistration(resolver, scraperCfg, logger)
}

func EffectiveConfig(scraperCfg config.ScraperConfig) config.ScraperPublishedAtResolverConfig {
	return effectivePublishedAtResolverConfig(scraperCfg)
}

func ValidateSchemaIfEnabled(
	ctx context.Context,
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	logger *slog.Logger,
) error {
	return validatePublishedAtResolverSchemaIfEnabled(ctx, scraperCfg, postgresService, logger)
}
