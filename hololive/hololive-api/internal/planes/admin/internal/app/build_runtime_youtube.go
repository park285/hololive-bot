package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

func buildAdminAPIYouTubeStack(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	logger *slog.Logger,
) *providers.YouTubeStack {
	return sharedmodules.BuildYouTubeAPIStack(ctx, &sharedmodules.YouTubeAPIStackParams{
		YouTubeConfig:   appConfig.YouTube,
		ScraperConfig:   appConfig.Scraper,
		CacheService:    infra.Cache,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})
}

func buildAdminAPICommunityShortsOpsRepository(infra *sharedmodules.InfraModule) server.YouTubeCommunityShortsOpsRepository {
	if infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil
	}
	return outbox.NewDeliveryTelemetryRepository(infra.Postgres.GetPool())
}
