package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type ingestionRuntimeYouTubeState struct {
	operationalChannels   []communityShortsOperationalChannel
	pollTargets           youtubePollTargets
	communityShortsPolicy communityShortsBigBangPolicy
	ingestionLease        *providers.IngestionLease
}

type ingestionRuntimeYouTubeDependencies struct {
	scraperScheduler    *poller.Scheduler
	outboxDispatcher    *outbox.Dispatcher
	publishedAtResolver *poller.PendingPublishedAtResolver
	pollerRegistrations []providers.ChannelPollerRegistration
	pollTargetRefresher *youTubePollTargetRefresher
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

	operationalChannels, err := resolveCommunityShortsOperationalChannelsFromRepository(ctx, infra.memberRepo)
	if err != nil {
		return state, fmt.Errorf("resolve community shorts operational channels: %w", err)
	}
	pollTargets, err := resolveYouTubePollTargets(ctx, infra.cacheService, infra.postgresService, operationalChannels, logger)
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
	if features.communityShortsBigBangEnabled {
		state.communityShortsPolicy, err = buildCommunityShortsBigBangPolicy(cfg.Ingestion, operationalChannels)
		if err != nil {
			return state, err
		}
		if state.communityShortsPolicy.Enabled() {
			logger.Info("Community/shorts big-bang request switch configured",
				slog.Time("community_shorts_bigbang_cutover_at", state.communityShortsPolicy.CutoverAt()),
				slog.Int("community_shorts_bigbang_target_channels", state.communityShortsPolicy.TargetChannelCount()),
			)
		} else {
			logger.Warn("Community/shorts big-bang request switch is missing cutover criteria",
				slog.Int("community_shorts_bigbang_target_channels", state.communityShortsPolicy.TargetChannelCount()))
		}
	}

	return state, nil
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

	routeDecider := buildCommunityShortsRouteDecider(state.communityShortsPolicy)
	sharedScraperClient := buildSharedYouTubeScraperClient(cfg.Scraper, infra.cacheService, infra.sharedRL)
	if err := validatePublishedAtResolverSchemaIfEnabled(ctx, cfg.Scraper, infra.postgresService, logger); err != nil {
		return deps, fmt.Errorf("validate published_at resolver schema: %w", err)
	}
	deps.publishedAtResolver = buildPendingPublishedAtResolver(
		cfg.Scraper,
		infra.postgresService,
		sharedScraperClient,
		routeDecider,
		logger,
	)
	var err error
	deps.scraperScheduler, deps.outboxDispatcher, deps.pollerRegistrations, err = buildStreamIngesterYouTubeComponents(
		cfg.Scraper,
		infra.postgresService,
		state.pollTargets.NotificationChannelIDs,
		state.pollTargets.StatsChannelIDs,
		sharedScraperClient,
		infra.cacheService,
		infra.irisClient,
		infra.templateRenderer,
		routeDecider,
		deps.publishedAtResolver,
		logger,
	)
	if err != nil {
		return deps, err
	}
	deps.pollTargetRefresher = newYouTubePollTargetRefresher(
		infra.cacheService,
		deps.scraperScheduler,
		deps.pollerRegistrations,
		state.operationalChannels,
		func(ctx context.Context) ([]string, error) {
			return loadAlarmChannelIDs(ctx, infra.postgresService)
		},
		logger,
	).withOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
		return resolveCommunityShortsOperationalChannelsFromRepository(ctx, infra.memberRepo)
	})
	deps.youtubeScheduler = infra.ytStack.Scheduler
	return deps, nil
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
