package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
)

// BuildStreamIngesterRuntime: stream-ingester 런타임을 구성합니다.
func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	if !cfg.Bot.IngestionEnabled {
		return nil, fmt.Errorf("stream ingester requires BOT_INGESTION_ENABLED=true")
	}
	logger.Info("Stream-ingester ingestion runtime enabled",
		slog.String("event", "stream_ingestion_enabled"),
		slog.String("env", "BOT_INGESTION_ENABLED=true"),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)

	infra, err := initStreamIngesterInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	ingestionLeaseRef, err := providers.AcquireIngestionLease(ctx, infra.cacheService, "stream-ingester", logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, fmt.Errorf("acquire ingestion lease: %w", err)
	}

	scraperScheduler, outboxDispatcher := buildStreamIngesterYouTubeComponents(
		cfg.Scraper,
		infra.postgresService,
		infra.membersData,
		infra.cacheService,
		infra.irisClient,
		infra.templateRenderer,
		infra.sharedRL,
		logger,
	)
	youtubeScheduler := infra.ytStack.Scheduler
	configSubscriber := buildStreamIngesterConfigSubscriber(
		infra.cacheService,
		infra.settingsService,
		infra.holodexService,
		infra.ytStack,
		scraperScheduler,
		logger,
	)

	desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
	applyScraperProxyToggle(
		desiredProxyState,
		ProvideYouTubeService(infra.ytStack),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return &StreamIngesterRuntime{
		Config:           cfg,
		Logger:           logger,
		Scheduler:        youtubeScheduler,
		ScraperScheduler: scraperScheduler,
		PhotoSync:        infra.photoSync,
		OutboxDispatcher: outboxDispatcher,
		ConfigSubscriber: configSubscriber,
		ServerAddr:       ProvideAPIAddr(cfg),
		HttpServer:       httpServer,
		ingestionLease:   ingestionLeaseRef,
		cleanup:          cleanup,
	}, nil
}

func buildStreamIngesterHTTPServer(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*http.Server, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("build stream ingester router: %w", err)
	}
	return ProvideAPIServer(ProvideAPIAddr(cfg), router), nil
}
