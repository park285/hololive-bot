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

package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

const (
	streamIngesterRuntimeName = "stream-ingester"
	youtubeScraperRuntimeName = "youtube-scraper"
)

var initStreamIngesterInfrastructureFn = initStreamIngesterInfrastructure

func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, streamIngesterSpec(cfg))
}

func BuildYouTubeScraperRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, youtubeScraperSpec(cfg))
}

func buildIngestionRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, spec ingestionRuntimeSpec) (*StreamIngesterRuntime, error) {
	if err := validateIngestionRuntimeInputs(cfg, logger); err != nil {
		return nil, err
	}

	logFeatureOverride(logger, spec)

	features := spec.features
	readiness := newIngestionReadinessState(spec.name, features)

	logIngestionRuntimeConfigured(logger, spec.name, features)

	infra, err := initStreamIngesterInfrastructureFn(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	youtubeState, err := resolveIngestionRuntimeYouTubeState(ctx, cfg, logger, spec, features, infra)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	if warnErr := observeSubscriberCacheOnYouTubeStartup(ctx, spec.name, features.youtubeEnabled, infra.cacheService, logger); warnErr != nil {
		logger.Warn("Failed to observe subscriber cache on startup",
			slog.String("runtime", spec.name),
			slog.Any("error", warnErr),
		)
	}
	if err := acquireIngestionLeaseIfEnabled(ctx, infra, logger, spec.name, features.youtubeEnabled, &youtubeState); err != nil {
		infra.cleanup()
		return nil, err
	}

	youtubeDeps, err := buildIngestionRuntimeYouTubeDependencies(ctx, cfg, logger, infra, features.youtubeEnabled, youtubeState)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	configSubscriber := buildRuntimeConfigSubscriber(features, infra, youtubeDeps.scraperScheduler, logger)
	observationWindowWriter := buildIngestionRuntimeObservationWindowWriter(spec.name, youtubeState.communityShortsPolicy, infra)

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger, spec.name, readiness)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	return newStreamIngesterRuntime(cfg, logger, spec.name, features, readiness, infra, youtubeState, youtubeDeps, configSubscriber, observationWindowWriter, httpServer), nil
}

func validateIngestionRuntimeInputs(cfg *config.Config, logger *slog.Logger) error {
	if cfg == nil {
		return fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return fmt.Errorf("logger must not be nil")
	}
	return nil
}

func logIngestionRuntimeConfigured(logger *slog.Logger, runtimeName string, features ingestionRuntimeFeatures) {
	logger.Info("Ingestion runtime configured",
		slog.String("runtime", runtimeName),
		slog.String("event", "ingestion_runtime_configured"),
		slog.Bool("youtube_enabled", features.youtubeEnabled),
		slog.Bool("photo_sync_enabled", features.photoSyncEnabled),
		slog.Bool("community_shorts_bigbang_enabled", features.communityShortsBigBangEnabled),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)
}

func acquireIngestionLeaseIfEnabled(
	ctx context.Context,
	infra *streamIngesterInfrastructure,
	logger *slog.Logger,
	runtimeName string,
	enabled bool,
	state *ingestionRuntimeYouTubeState,
) error {
	if !enabled {
		return nil
	}
	lease, err := providers.AcquireIngestionLease(ctx, infra.cacheService, runtimeName, logger)
	if err != nil {
		return fmt.Errorf("acquire ingestion lease: %w", err)
	}
	state.ingestionLease = lease
	return nil
}

func newStreamIngesterRuntime(
	cfg *config.Config,
	logger *slog.Logger,
	runtimeName string,
	features ingestionRuntimeFeatures,
	readiness *ingestionReadinessState,
	infra *streamIngesterInfrastructure,
	youtubeState ingestionRuntimeYouTubeState,
	youtubeDeps ingestionRuntimeYouTubeDependencies,
	configSubscriber *configsub.Subscriber,
	observationWindowWriter communityShortsObservationWindowWriter,
	httpServer *http.Server,
) *StreamIngesterRuntime {
	cleanup := func() {
		infra.cleanup()
	}

	return &StreamIngesterRuntime{
		RuntimeName:                            runtimeName,
		Config:                                 cfg,
		Logger:                                 logger,
		Scheduler:                              youtubeDeps.youtubeScheduler,
		ScraperScheduler:                       youtubeDeps.scraperScheduler,
		PublishedAtResolver:                    youtubeDeps.publishedAtResolver,
		PhotoSync:                              selectPhotoSyncService(features.photoSyncEnabled, infra.photoSync),
		OutboxDispatcher:                       youtubeDeps.outboxDispatcher,
		ConfigSubscriber:                       configSubscriber,
		PollTargetRefresher:                    youtubeDeps.pollTargetRefresher,
		ServerAddr:                             fmt.Sprintf(":%d", cfg.Server.Port),
		HttpServer:                             httpServer,
		Readiness:                              readiness,
		CommunityShortsBigBangPolicy:           youtubeState.communityShortsPolicy,
		communityShortsObservationWindowWriter: observationWindowWriter,
		ingestionLease:                         youtubeState.ingestionLease,
		Managed:                                lifecycle.NewManaged(cleanup),
	}
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
