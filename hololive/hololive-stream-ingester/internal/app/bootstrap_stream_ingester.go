// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

const (
	streamIngesterRuntimeName = "stream-ingester"
	youtubeScraperRuntimeName = "youtube-scraper"
)

type ingestionRuntimeFeatures struct {
	youtubeEnabled   bool
	photoSyncEnabled bool
}

type ingestionRuntimeSpec struct {
	name              string
	requestedFeatures ingestionRuntimeFeatures
	features          ingestionRuntimeFeatures
}

// BuildStreamIngesterRuntime: stream-ingester 런타임을 구성합니다.
func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, streamIngesterSpec(cfg))
}

// BuildYouTubeScraperRuntime: youtube-scraper 런타임을 구성합니다.
func BuildYouTubeScraperRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, youtubeScraperSpec(cfg))
}

func buildIngestionRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, spec ingestionRuntimeSpec) (*StreamIngesterRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	logFeatureOverride(logger, spec)

	features := spec.features
	readiness := newIngestionReadinessState(spec.name, features)

	logger.Info("Ingestion runtime configured",
		slog.String("runtime", spec.name),
		slog.String("event", "ingestion_runtime_configured"),
		slog.Bool("youtube_enabled", features.youtubeEnabled),
		slog.Bool("photo_sync_enabled", features.photoSyncEnabled),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)

	infra, err := initStreamIngesterInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	var ingestionLeaseRef *providers.IngestionLease
	if features.youtubeEnabled {
		ingestionLeaseRef, err = providers.AcquireIngestionLease(ctx, infra.cacheService, spec.name, logger)
		if err != nil {
			infra.cleanup()
			return nil, fmt.Errorf("acquire ingestion lease: %w", err)
		}
	}

	var scraperScheduler *poller.Scheduler
	var outboxDispatcher *outbox.Dispatcher
	var youtubeScheduler youtube.Scheduler
	if features.youtubeEnabled {
		scraperScheduler, outboxDispatcher = buildStreamIngesterYouTubeComponents(
			cfg.Scraper,
			infra.postgresService,
			infra.membersData,
			infra.cacheService,
			infra.irisClient,
			infra.templateRenderer,
			infra.sharedRL,
			logger,
		)
		youtubeScheduler = infra.ytStack.Scheduler
	}

	configSubscriber := buildRuntimeConfigSubscriber(features, infra, scraperScheduler, logger)

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger, spec.name, readiness)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	cleanup := func() {
		infra.cleanup()
	}

	return &StreamIngesterRuntime{
		RuntimeName:      spec.name,
		Config:           cfg,
		Logger:           logger,
		Scheduler:        youtubeScheduler,
		ScraperScheduler: scraperScheduler,
		PhotoSync:        selectPhotoSyncService(features.photoSyncEnabled, infra.photoSync),
		OutboxDispatcher: outboxDispatcher,
		ConfigSubscriber: configSubscriber,
		ServerAddr:       fmt.Sprintf(":%d", cfg.Server.Port),
		HttpServer:       httpServer,
		Readiness:        readiness,
		ingestionLease:   ingestionLeaseRef,
		Managed:          lifecycle.NewManaged(cleanup),
	}, nil
}

func buildStreamIngesterHTTPServer(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	runtimeName string,
	readiness *ingestionReadinessState,
) (*http.Server, error) {
	router, err := sharedserver.NewHealthOnlyRuntimeRouter(ctx, logger, cfg.Server.APIKey, func(opts *sharedserver.RuntimeRouterOptions) {
		opts.EnableGzip = true
		opts.ReadyResponder = func(c *gin.Context) {
			statusCode, payload := readiness.response()
			c.JSON(statusCode, payload)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("build stream ingester router: %w", err)
	}
	return sharedserver.NewH2CServer(
		fmt.Sprintf(":%d", cfg.Server.Port),
		router,
		runtimeHTTPServerOperationName(runtimeName),
	), nil
}

func streamIngesterSpec(cfg *config.Config) ingestionRuntimeSpec {
	requested := requestedFeatures(cfg)

	return ingestionRuntimeSpec{
		name:              streamIngesterRuntimeName,
		requestedFeatures: requested,
		features:          requested,
	}
}

func youtubeScraperSpec(cfg *config.Config) ingestionRuntimeSpec {
	return ingestionRuntimeSpec{
		name:              youtubeScraperRuntimeName,
		requestedFeatures: requestedFeatures(cfg),
		features: ingestionRuntimeFeatures{
			youtubeEnabled:   true,
			photoSyncEnabled: false,
		},
	}
}

func requestedFeatures(cfg *config.Config) ingestionRuntimeFeatures {
	if cfg == nil {
		return ingestionRuntimeFeatures{}
	}

	return ingestionRuntimeFeatures{
		youtubeEnabled:   cfg.Ingestion.YouTubeEnabled,
		photoSyncEnabled: cfg.Ingestion.PhotoSyncEnabled,
	}
}

func logFeatureOverride(logger *slog.Logger, spec ingestionRuntimeSpec) {
	if logger == nil {
		return
	}
	if spec.requestedFeatures == spec.features {
		return
	}

	logger.Warn("YouTube scraper runtime overrides ingestion feature toggles",
		slog.String("runtime", spec.name),
		slog.Bool("requested_youtube_enabled", spec.requestedFeatures.youtubeEnabled),
		slog.Bool("effective_youtube_enabled", spec.features.youtubeEnabled),
		slog.Bool("requested_photo_sync_enabled", spec.requestedFeatures.photoSyncEnabled),
		slog.Bool("effective_photo_sync_enabled", spec.features.photoSyncEnabled),
	)
}

func selectPhotoSyncService(enabled bool, service *holodex.PhotoSyncService) *holodex.PhotoSyncService {
	if !enabled {
		return nil
	}
	return service
}

func buildRuntimeConfigSubscriber(
	features ingestionRuntimeFeatures,
	infra *streamIngesterInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	if !features.youtubeEnabled && !features.photoSyncEnabled {
		return nil
	}

	configSubscriber := buildStreamIngesterConfigSubscriber(
		infra.cacheService,
		infra.settingsService,
		infra.holodexService,
		infra.ytStack,
		scraperScheduler,
		logger,
	)

	desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
	sharedsettings.ApplyScraperProxyToggle(
		desiredProxyState,
		infra.ytStack.GetService(),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	return configSubscriber
}
