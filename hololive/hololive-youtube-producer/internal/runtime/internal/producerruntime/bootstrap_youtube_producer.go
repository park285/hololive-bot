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

package producerruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/alarmcache"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
	sharedlog "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

const (
	youtubeProducerRuntimeName = "youtube-producer"

	youtubeProducerRuntimeAllowedEnv = "YOUTUBE_PRODUCER_RUNTIME_ALLOWED"
)

var initYouTubeProducerInfrastructureFn = initYouTubeProducerInfrastructure

func BuildYouTubeProducerRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*YouTubeProducerRuntime, error) {
	if err := validateIngestionRuntimeInputs(appConfig, logger); err != nil {
		return nil, err
	}
	if !youtubeProducerRuntimeAllowed() {
		return nil, fmt.Errorf("youtube producer runtime disabled: set %s=true on the owning host", youtubeProducerRuntimeAllowedEnv)
	}
	return buildIngestionRuntime(ctx, appConfig, logger, youtubeProducerSpec(appConfig))
}

func youtubeProducerRuntimeAllowed() bool {
	allowed, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(youtubeProducerRuntimeAllowedEnv)))
	return err == nil && allowed
}

func buildIngestionRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger, spec ingestionRuntimeSpec) (*YouTubeProducerRuntime, error) {
	if err := validateIngestionRuntimeInputs(appConfig, logger); err != nil {
		return nil, err
	}

	logFeatureOverride(logger, spec)

	features := spec.features
	readinessState := newReadinessStateWithFetcherEngine(spec.name, features, appConfig.Scraper.FetcherEngine)

	logIngestionRuntimeConfigured(ctx, logger, spec.name, features, appConfig.Scraper.FetcherEngine)

	infra, err := initYouTubeProducerInfrastructureFn(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	youtubeState, err := resolveIngestionRuntimeYouTubeState(ctx, appConfig, logger, features, infra)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	if warnErr := alarmcache.ObserveSubscriberCacheOnProducerStartup(ctx, spec.name, features.youtubeEnabled, infra.cacheService, logger); warnErr != nil {
		logger.Warn("Failed to observe subscriber cache on startup",
			slog.String("runtime", spec.name),
			slog.Any("error", warnErr),
		)
	}
	if err := acquireIngestionLeaseIfEnabled(ctx, infra, logger, spec.name, features.youtubeEnabled && !features.activeActiveEnabled, &youtubeState); err != nil {
		infra.cleanup()
		return nil, err
	}

	youtubeDeps, err := buildIngestionRuntimeYouTubeDependencies(ctx, appConfig, logger, infra, features.youtubeEnabled, &youtubeState, readinessState)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	configSubscriber := buildRuntimeConfigSubscriber(features, infra, youtubeDeps.scraperScheduler, logger)
	observationWindowWriter := buildIngestionRuntimeObservationWindowWriter(spec.name, youtubeState.communityShortsPolicy, infra)

	httpServers, err := buildYouTubeProducerHTTPServers(ctx, appConfig, logger, spec.name, readinessState)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	return newYouTubeProducerRuntime(appConfig, logger, spec.name, features, readinessState, infra, &youtubeState, youtubeDeps, configSubscriber, observationWindowWriter, httpServers), nil
}

func validateIngestionRuntimeInputs(appConfig *config.Config, logger *slog.Logger) error {
	if appConfig == nil {
		return fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return fmt.Errorf("logger must not be nil")
	}
	return nil
}

func logIngestionRuntimeConfigured(ctx context.Context, logger *slog.Logger, runtimeName string, features ingestionRuntimeFeatures, scraperFetcherEngine string) {
	sharedlog.Info(ctx, logger, EventIngestionRuntimeConfigured, "ingestion runtime configured",
		sharedlog.Runtime(runtimeName),
		slog.Bool("youtube_enabled", features.youtubeEnabled),
		slog.Bool("photo_sync_enabled", features.photoSyncEnabled),
		slog.Bool("community_shorts_bigbang_enabled", features.communityShortsBigBangEnabled),
		slog.Bool("active_active_enabled", features.activeActiveEnabled),
		slog.String("scraper_fetcher_engine", normalizeScraperFetcherEngineForLog(scraperFetcherEngine)),
		slog.String("lock_key", ingestionlease.Key),
	)
}

func normalizeScraperFetcherEngineForLog(engine string) string {
	engine = strings.TrimSpace(engine)
	if engine == "" {
		return "nethttp"
	}
	return engine
}

func acquireIngestionLeaseIfEnabled(
	ctx context.Context,
	infra *youtubeProducerInfrastructure,
	logger *slog.Logger,
	runtimeName string,
	enabled bool,
	state *ingestionRuntimeYouTubeState,
) error {
	if !enabled {
		return nil
	}
	lease, err := ingestionlease.Acquire(ctx, infra.cacheService, runtimeName, logger)
	if err != nil {
		return fmt.Errorf("acquire ingestion lease: %w", err)
	}
	state.ingestionLease = lease
	return nil
}

func newYouTubeProducerRuntime(
	appConfig *config.Config,
	logger *slog.Logger,
	runtimeName string,
	features ingestionRuntimeFeatures,
	readinessState *readiness.State,
	infra *youtubeProducerInfrastructure,
	youtubeState *ingestionRuntimeYouTubeState,
	youtubeDeps ingestionRuntimeYouTubeDependencies,
	configSubscriber *configsub.Subscriber,
	observationWindowWriter communityShortsObservationWindowWriter,
	httpServers *sharedserver.RuntimeHTTPServers,
) *YouTubeProducerRuntime {
	cleanup := func() {
		infra.cleanup()
	}

	return &YouTubeProducerRuntime{
		RuntimeName:                            runtimeName,
		Config:                                 appConfig,
		Logger:                                 logger,
		Scheduler:                              youtubeDeps.youtubeScheduler,
		ScraperScheduler:                       youtubeDeps.scraperScheduler,
		PublishedAtResolver:                    youtubeDeps.publishedAtResolver,
		PhotoSync:                              buildRuntimePhotoSyncService(appConfig, features, infra, logger),
		ConfigSubscriber:                       configSubscriber,
		PollTargetRefresher:                    youtubeDeps.pollTargetRefresher,
		ServerAddr:                             httpServers.Addr(),
		HTTPServers:                            httpServers,
		Readiness:                              readinessState,
		CommunityShortsBigBangPolicy:           youtubeState.communityShortsPolicy,
		communityShortsObservationWindowWriter: observationWindowWriter,
		ingestionLease:                         youtubeState.ingestionLease,
		Managed:                                lifecycle.NewManaged(cleanup),
	}
}

func buildRuntimePhotoSyncService(
	appConfig *config.Config,
	features ingestionRuntimeFeatures,
	infra *youtubeProducerInfrastructure,
	logger *slog.Logger,
) photoSyncService {
	if !features.photoSyncEnabled || infra.photoSync == nil {
		return nil
	}
	service := infra.photoSync
	if !appConfig.Scraper.ActiveActive.Enabled {
		return service
	}
	guard := ingestionlease.NewJobRunGuard(infra.cacheService, ingestionlease.JobRunGuardConfig{
		Namespace:  appConfig.Scraper.ActiveActive.Namespace,
		InstanceID: appConfig.Scraper.ActiveActive.InstanceID,
	})
	return newLeasedPhotoSyncService(service, guard, logger)
}

func buildYouTubeProducerHTTPServer(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	readinessState *readiness.State,
) (*http.Server, error) {
	router, err := buildYouTubeProducerHTTPRouter(ctx, appConfig, logger, readinessState)
	if err != nil {
		return nil, fmt.Errorf("build youtube producer router: %w", err)
	}
	return sharedserver.NewH2CServer(
		fmt.Sprintf(":%d", appConfig.Server.Port),
		router,
		readiness.HTTPServerOperationName(youtubeProducerRuntimeName),
	), nil
}

func buildYouTubeProducerHTTPServers(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	runtimeName string,
	readinessState *readiness.State,
) (*sharedserver.RuntimeHTTPServers, error) {
	router, err := buildYouTubeProducerHTTPRouter(ctx, appConfig, logger, readinessState)
	if err != nil {
		return nil, fmt.Errorf("build youtube producer router: %w", err)
	}
	return sharedserver.NewRuntimeHTTPServers(
		&appConfig.Server,
		router,
		readiness.HTTPServerOperationName(runtimeName),
	)
}

func buildYouTubeProducerHTTPRouter(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	readinessState *readiness.State,
) (http.Handler, error) {
	return sharedserver.NewHealthOnlyRuntimeRouter(ctx, logger, appConfig.Server.APIKey, func(opts *sharedserver.RuntimeRouterOptions) {
		opts.EnableGzip = true
		opts.ReadyResponder = func(c *gin.Context) {
			statusCode, payload := readinessState.PublicResponse()
			c.JSON(statusCode, payload)
		}
	})
}
