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
	scraperConfig config.ScraperConfig,
	postgresService database.Client,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	return buildPendingPublishedAtResolver(scraperConfig, postgresService, scraperClient, routeDecider, logger)
}

func BuildRegistration(
	resolver *poller.PendingPublishedAtResolver,
	scraperConfig config.ScraperConfig,
	logger *slog.Logger,
) *providers.ChannelPollerRegistration {
	return buildPublishedAtResolverRegistration(resolver, scraperConfig, logger)
}

func EffectiveConfig(scraperConfig config.ScraperConfig) config.ScraperPublishedAtResolverConfig {
	return effectivePublishedAtResolverConfig(scraperConfig)
}

func ValidateSchemaIfEnabled(
	ctx context.Context,
	scraperConfig config.ScraperConfig,
	postgresService database.Client,
	logger *slog.Logger,
) error {
	return validatePublishedAtResolverSchemaIfEnabled(ctx, scraperConfig, postgresService, logger)
}
