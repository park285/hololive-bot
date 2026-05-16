package ingesterruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/polling"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/polltarget"
	"github.com/kapu/hololive-stream-ingester/internal/runtime/publishedat"
)

type ingestionRuntimeYouTubeState struct {
	operationalChannels   []communityShortsOperationalChannel
	pollTargets           polltarget.Targets
	communityShortsPolicy communityShortsBigBangPolicy
	ingestionLease        *ingestionlease.Lease
}

type ingestionRuntimeYouTubeDependencies struct {
	scraperScheduler    *poller.Scheduler
	publishedAtResolver *poller.PendingPublishedAtResolver
	pollerRegistrations []providers.ChannelPollerRegistration
	pollTargetRefresher *polltarget.Refresher
	youtubeScheduler    youtube.Scheduler
}

func resolveIngestionRuntimeYouTubeState(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	spec ingestionRuntimeSpec,
	features ingestionRuntimeFeatures,
	infra *streamIngesterInfrastructure,
) (ingestionRuntimeYouTubeState, error) {
	state := ingestionRuntimeYouTubeState{}
	if !features.youtubeEnabled {
		return state, nil
	}

	operationalChannels, err := communityshorts.ResolveOperationalChannelsFromRepository(ctx, infra.memberRepo)
	if err != nil {
		return state, fmt.Errorf("resolve community shorts operational channels: %w", err)
	}
	pollTargets, err := polltarget.Resolve(ctx, infra.cacheService, infra.postgresService, operationalChannels, logger)
	if err != nil {
		return state, err
	}

	logger.Info("Resolved YouTube poll targets",
		slog.Int("notification_target_channels", len(pollTargets.NotificationChannelIDs)),
		slog.Int("stats_target_channels", len(pollTargets.StatsChannelIDs)),
		slog.Int("dropped_alarm_targets", pollTargets.DroppedAlarmTargets),
	)

	state.operationalChannels = operationalChannels
	state.pollTargets = pollTargets
	communityShortsPolicy, err := resolveCommunityShortsBigBangPolicy(cfg, logger, operationalChannels, features)
	if err != nil {
		return state, err
	}
	state.communityShortsPolicy = communityShortsPolicy

	return state, nil
}

func resolveCommunityShortsBigBangPolicy(
	cfg *config.Config,
	logger *slog.Logger,
	operationalChannels []communityShortsOperationalChannel,
	features ingestionRuntimeFeatures,
) (communityShortsBigBangPolicy, error) {
	if !features.communityShortsBigBangEnabled {
		return communityShortsBigBangPolicy{}, nil
	}

	policy, err := communityshorts.BuildPolicy(cfg.Ingestion, operationalChannels)
	if err != nil {
		return policy, err
	}
	if policy.Enabled() {
		logger.Info("Community/shorts big-bang request switch configured",
			slog.Time("community_shorts_bigbang_cutover_at", policy.CutoverAt()),
			slog.Int("community_shorts_bigbang_target_channels", policy.TargetChannelCount()),
		)
		return policy, nil
	}

	logger.Warn("Community/shorts big-bang request switch is missing cutover criteria",
		slog.Int("community_shorts_bigbang_target_channels", policy.TargetChannelCount()))
	return policy, nil
}

func buildIngestionRuntimeYouTubeDependencies(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	infra *streamIngesterInfrastructure,
	enabled bool,
	state ingestionRuntimeYouTubeState,
) (ingestionRuntimeYouTubeDependencies, error) {
	deps := ingestionRuntimeYouTubeDependencies{}
	if !enabled {
		return deps, nil
	}

	routeDecider := communityshorts.BuildRouteDecider(state.communityShortsPolicy)
	sharedScraperClient := resolveIngestionSharedScraperClient(cfg.Scraper, infra)
	if err := publishedat.ValidateSchemaIfEnabled(ctx, cfg.Scraper, infra.postgresService, logger); err != nil {
		return deps, fmt.Errorf("validate published_at resolver schema: %w", err)
	}
	deps.publishedAtResolver = publishedat.BuildPendingResolver(
		cfg.Scraper,
		infra.postgresService,
		sharedScraperClient,
		routeDecider,
		logger,
	)
	var err error
	deps.scraperScheduler, deps.pollerRegistrations, err = polling.BuildComponents(
		cfg.Scraper,
		infra.postgresService,
		state.pollTargets.NotificationChannelIDs,
		state.pollTargets.StatsChannelIDs,
		sharedScraperClient,
		infra.holodexService,
		routeDecider,
		deps.publishedAtResolver,
		logger,
	)
	if err != nil {
		return deps, err
	}
	deps.pollTargetRefresher = polltarget.NewRefresher(
		infra.cacheService,
		deps.scraperScheduler,
		deps.pollerRegistrations,
		state.operationalChannels,
		func(ctx context.Context) ([]string, error) {
			return polltarget.LoadAlarmChannelIDs(ctx, infra.postgresService)
		},
		logger,
	).WithTieringDB(infra.postgresService.GetGormDB()).WithOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
		return communityshorts.ResolveOperationalChannelsFromRepository(ctx, infra.memberRepo)
	})
	deps.youtubeScheduler = infra.ytStack.Scheduler
	return deps, nil
}

func resolveIngestionSharedScraperClient(scraperCfg config.ScraperConfig, infra *streamIngesterInfrastructure) *scraper.Client {
	if infra.scraperClient != nil {
		return infra.scraperClient
	}
	return polling.BuildSharedClient(scraperCfg, infra.cacheService, infra.sharedRL)
}

func buildIngestionRuntimeObservationWindowWriter(
	runtimeName string,
	policy communityShortsBigBangPolicy,
	infra *streamIngesterInfrastructure,
) communityShortsObservationWindowWriter {
	if runtimeName == youtubeScraperRuntimeName && policy.Enabled() {
		return trackingrepo.NewRepository(infra.postgresService.GetGormDB())
	}
	return nil
}
