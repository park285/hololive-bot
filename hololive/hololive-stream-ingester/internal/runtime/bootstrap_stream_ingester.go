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
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

const (
	streamIngesterRuntimeName = "stream-ingester"
	youtubeScraperRuntimeName = "youtube-scraper"
)

type ingestionRuntimeFeatures struct {
	youtubeEnabled                bool
	photoSyncEnabled              bool
	communityShortsBigBangEnabled bool
}

type ingestionRuntimeSpec struct {
	name              string
	requestedFeatures ingestionRuntimeFeatures
	features          ingestionRuntimeFeatures
}

func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, streamIngesterSpec(cfg))
}

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
		slog.Bool("community_shorts_bigbang_enabled", features.communityShortsBigBangEnabled),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)

	infra, err := initStreamIngesterInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	var operationalChannels []communityShortsOperationalChannel
	var ytPollTargets youtubePollTargets
	if features.youtubeEnabled {
		operationalChannels, err = resolveCommunityShortsOperationalChannels(infra.membersData)
		if err != nil {
			infra.cleanup()
			return nil, fmt.Errorf("resolve community shorts operational channels: %w", err)
		}

		ytPollTargets, err = resolveYouTubePollTargets(ctx, infra.cacheService, infra.postgresService, operationalChannels, logger)
		if err != nil {
			infra.cleanup()
			return nil, err
		}

		logger.Info("Resolved YouTube poll targets",
			slog.Int("notification_target_channels", len(ytPollTargets.NotificationChannelIDs)),
			slog.Int("stats_target_channels", len(ytPollTargets.StatsChannelIDs)),
			slog.Int("dropped_alarm_targets", ytPollTargets.DroppedAlarmTargets),
		)
	}

	var communityShortsPolicy communityShortsBigBangPolicy
	if features.youtubeEnabled && features.communityShortsBigBangEnabled {
		communityShortsPolicy, err = buildCommunityShortsBigBangPolicy(cfg.Ingestion, operationalChannels)
		if err != nil {
			infra.cleanup()
			return nil, err
		}
		if communityShortsPolicy.Enabled() {
			logger.Info("Community/shorts big-bang request switch configured",
				slog.Time("community_shorts_bigbang_cutover_at", communityShortsPolicy.CutoverAt()),
				slog.Int("community_shorts_bigbang_target_channels", communityShortsPolicy.TargetChannelCount()),
			)
		} else {
			logger.Warn("Community/shorts big-bang request switch is missing cutover criteria",
				slog.Int("community_shorts_bigbang_target_channels", communityShortsPolicy.TargetChannelCount()))
		}
	}

	if warnErr := observeSubscriberCacheOnYouTubeStartup(ctx, spec.name, features.youtubeEnabled, infra.cacheService, logger); warnErr != nil {
		logger.Warn("Failed to observe subscriber cache on startup",
			slog.String("runtime", spec.name),
			slog.Any("error", warnErr),
		)
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
	var publishedAtResolver *poller.PendingPublishedAtResolver
	var pollerRegistrations []providers.ChannelPollerRegistration
	var pollTargetRefresher *youTubePollTargetRefresher
	var youtubeScheduler youtube.Scheduler
	if features.youtubeEnabled {
		routeDecider := buildCommunityShortsRouteDecider(communityShortsPolicy)
		sharedScraperClient := buildSharedYouTubeScraperClient(cfg.Scraper, infra.cacheService, infra.sharedRL)
		if err := validatePublishedAtResolverSchema(ctx, infra.postgresService); err != nil {
			infra.cleanup()
			return nil, fmt.Errorf("validate published_at resolver schema: %w", err)
		}
		logger.Info("published_at_resolver_schema_validated")
		scraperScheduler, outboxDispatcher, pollerRegistrations, err = buildStreamIngesterYouTubeComponents(
			cfg.Scraper,
			infra.postgresService,
			ytPollTargets.NotificationChannelIDs,
			ytPollTargets.StatsChannelIDs,
			sharedScraperClient,
			infra.cacheService,
			infra.irisClient,
			infra.templateRenderer,
			routeDecider,
			logger,
		)
		if err != nil {
			infra.cleanup()
			return nil, err
		}
		publishedAtResolver = buildPendingPublishedAtResolver(
			cfg.Scraper,
			infra.postgresService,
			sharedScraperClient,
			routeDecider,
			logger,
		)
		pollTargetRefresher = newYouTubePollTargetRefresher(
			infra.cacheService,
			scraperScheduler,
			pollerRegistrations,
			operationalChannels,
			func(ctx context.Context) ([]string, error) {
				return loadAlarmChannelIDs(ctx, infra.postgresService)
			},
			logger,
		).withOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
			return resolveCommunityShortsOperationalChannels(infra.membersData)
		})
		youtubeScheduler = infra.ytStack.Scheduler
	}

	configSubscriber := buildRuntimeConfigSubscriber(features, infra, scraperScheduler, logger)

	var observationWindowWriter communityShortsObservationWindowWriter
	if spec.name == youtubeScraperRuntimeName && communityShortsPolicy.Enabled() {
		observationWindowWriter = trackingrepo.NewRepository(infra.postgresService.GetGormDB())
	}

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger, spec.name, readiness)
	if err != nil {
		infra.cleanup()
		return nil, err
	}

	cleanup := func() {
		infra.cleanup()
	}

	return &StreamIngesterRuntime{
		RuntimeName:                            spec.name,
		Config:                                 cfg,
		Logger:                                 logger,
		Scheduler:                              youtubeScheduler,
		ScraperScheduler:                       scraperScheduler,
		PublishedAtResolver:                    publishedAtResolver,
		PhotoSync:                              selectPhotoSyncService(features.photoSyncEnabled, infra.photoSync),
		OutboxDispatcher:                       outboxDispatcher,
		ConfigSubscriber:                       configSubscriber,
		PollTargetRefresher:                    pollTargetRefresher,
		ServerAddr:                             fmt.Sprintf(":%d", cfg.Server.Port),
		HttpServer:                             httpServer,
		Readiness:                              readiness,
		CommunityShortsBigBangPolicy:           communityShortsPolicy,
		communityShortsObservationWindowWriter: observationWindowWriter,
		ingestionLease:                         ingestionLeaseRef,
		Managed:                                lifecycle.NewManaged(cleanup),
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
	features := requested
	if requested.communityShortsBigBangEnabled {
		features.youtubeEnabled = false
		features.communityShortsBigBangEnabled = false
	}

	return ingestionRuntimeSpec{
		name:              streamIngesterRuntimeName,
		requestedFeatures: requested,
		features:          features,
	}
}

func youtubeScraperSpec(cfg *config.Config) ingestionRuntimeSpec {
	requested := requestedFeatures(cfg)

	return ingestionRuntimeSpec{
		name:              youtubeScraperRuntimeName,
		requestedFeatures: requested,
		features: ingestionRuntimeFeatures{
			youtubeEnabled:                requested.communityShortsBigBangEnabled,
			photoSyncEnabled:              false,
			communityShortsBigBangEnabled: requested.communityShortsBigBangEnabled,
		},
	}
}

func requestedFeatures(cfg *config.Config) ingestionRuntimeFeatures {
	if cfg == nil {
		return ingestionRuntimeFeatures{}
	}

	return ingestionRuntimeFeatures{
		youtubeEnabled:                cfg.Ingestion.YouTubeEnabled,
		photoSyncEnabled:              cfg.Ingestion.PhotoSyncEnabled,
		communityShortsBigBangEnabled: cfg.Ingestion.CommunityShortsBigBangEnabled,
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
		slog.Bool("requested_community_shorts_bigbang_enabled", spec.requestedFeatures.communityShortsBigBangEnabled),
		slog.Bool("effective_community_shorts_bigbang_enabled", spec.features.communityShortsBigBangEnabled),
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
