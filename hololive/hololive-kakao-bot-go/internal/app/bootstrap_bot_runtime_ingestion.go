package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type botRuntimeIngestionComponents struct {
	scheduler        youtube.Scheduler
	scraperScheduler *poller.Scheduler
	photoSyncService *holodex.PhotoSyncService
	outboxDispatcher *outbox.Dispatcher
	ingestionLease   *providers.IngestionLease
}

func buildBotRuntimeIngestion(
	ctx context.Context,
	cfg *config.Config,
	runtimeViews botRuntimeDependencyViews,
	logger *slog.Logger,
) (botRuntimeIngestionComponents, error) {
	var components botRuntimeIngestionComponents
	if cfg == nil {
		return components, fmt.Errorf("build bot runtime ingestion: config is nil")
	}

	if !cfg.Bot.IngestionEnabled {
		logger.Info("Bot ingestion runtime disabled by config", slog.String("env", "BOT_INGESTION_ENABLED=false"))
		return components, nil
	}

	ingestionDeps := runtimeViews.ingestion
	if ingestionDeps.cache == nil || ingestionDeps.postgres == nil || ingestionDeps.irisClient == nil || ingestionDeps.members == nil || ingestionDeps.settings == nil {
		return components, fmt.Errorf("build bot runtime ingestion: dependency view is incomplete")
	}

	logger.Warn("Bot ingestion runtime enabled",
		slog.String("event", "bot_ingestion_enabled"),
		slog.String("env", "BOT_INGESTION_ENABLED=true"),
		slog.String("lock_key", providers.IngestionLeaseKey),
		slog.String("note", "when stream-ingester is deployed, bot should usually run with BOT_INGESTION_ENABLED=false"),
	)

	lease, err := providers.AcquireIngestionLease(ctx, ingestionDeps.cache, "bot", logger)
	if err != nil {
		return components, fmt.Errorf("acquire ingestion lease: %w", err)
	}

	youTubeRuntimeDeps := runtimeViews.youtubeRuntime
	scraperScheduler, outboxDispatcher := buildYouTubeComponents(cfg.Scraper, ingestionDeps, youTubeRuntimeDeps, logger)
	desiredProxyState := ingestionDeps.settings.Get().ScraperProxyEnabled
	applyScraperProxyToggle(desiredProxyState, youTubeRuntimeDeps.youtubeService, youTubeRuntimeDeps.holodexService, scraperScheduler, logger)

	components.scheduler = ingestionDeps.scheduler
	components.scraperScheduler = scraperScheduler
	components.photoSyncService = youTubeRuntimeDeps.photoSyncService
	components.outboxDispatcher = outboxDispatcher
	components.ingestionLease = lease
	return components, nil
}
