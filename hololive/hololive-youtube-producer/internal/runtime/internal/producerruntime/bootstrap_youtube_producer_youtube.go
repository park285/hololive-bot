package producerruntime

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/publishedat"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

const activeActivePollTargetRefreshMaxJitter = 2 * time.Second

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
	appConfig *config.Config,
	logger *slog.Logger,
	spec ingestionRuntimeSpec,
	features ingestionRuntimeFeatures,
	infra *youtubeProducerInfrastructure,
) (ingestionRuntimeYouTubeState, error) {
	state := ingestionRuntimeYouTubeState{}
	if !features.youtubeEnabled {
		return state, nil
	}

	operationalChannels, err := communityshorts.ResolveOperationalChannelsFromRepository(ctx, infra.memberRepository)
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
	communityShortsPolicy, err := resolveCommunityShortsBigBangPolicy(appConfig, logger, operationalChannels, features)
	if err != nil {
		return state, err
	}
	state.communityShortsPolicy = communityShortsPolicy

	return state, nil
}

func resolveCommunityShortsBigBangPolicy(
	appConfig *config.Config,
	logger *slog.Logger,
	operationalChannels []communityShortsOperationalChannel,
	features ingestionRuntimeFeatures,
) (communityShortsBigBangPolicy, error) {
	if !features.communityShortsBigBangEnabled {
		return communityShortsBigBangPolicy{}, nil
	}

	policy, err := communityshorts.BuildPolicy(appConfig.Ingestion, operationalChannels)
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
	appConfig *config.Config,
	logger *slog.Logger,
	infra *youtubeProducerInfrastructure,
	enabled bool,
	state ingestionRuntimeYouTubeState,
	readinessState *readiness.State,
) (ingestionRuntimeYouTubeDependencies, error) {
	deps := ingestionRuntimeYouTubeDependencies{}
	if !enabled {
		return deps, nil
	}

	routeDecider := communityshorts.BuildRouteDecider(state.communityShortsPolicy)
	sharedScraperClient := resolveIngestionSharedScraperClient(appConfig.Scraper, infra)
	if err := publishedat.ValidateSchemaIfEnabled(ctx, appConfig.Scraper, infra.postgresService, logger); err != nil {
		return deps, fmt.Errorf("validate published_at resolver schema: %w", err)
	}
	jobClaimer, err := buildIngestionRuntimeJobClaimer(appConfig, infra)
	if err != nil {
		return deps, err
	}
	jobClaimer = newReadinessReportingJobClaimer(jobClaimer, readinessState)
	if appConfig.Scraper.ActiveActive.Enabled {
		probeReadinessJobClaimer(ctx, jobClaimer, logger)
	}
	deps.publishedAtResolver = publishedat.BuildPendingResolver(
		appConfig.Scraper,
		infra.postgresService,
		sharedScraperClient,
		routeDecider,
		logger,
	)
	if deps.publishedAtResolver != nil {
		deps.publishedAtResolver.SetCandidateClaimer(jobClaimer)
	}
	deps.scraperScheduler, deps.pollerRegistrations, err = polling.BuildComponentsWithJobClaimer(
		appConfig.Scraper,
		jobClaimer,
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
	startActiveActiveRecoveryLoopIfEnabled(ctx, appConfig, jobClaimer, readinessState, deps.scraperScheduler, logger)
	deps.pollTargetRefresher = buildPollTargetRefresher(appConfig, infra, deps, state, logger)
	deps.youtubeScheduler = infra.ytStack.Scheduler
	return deps, nil
}

func startActiveActiveRecoveryLoopIfEnabled(
	ctx context.Context,
	appConfig *config.Config,
	jobClaimer poller.JobClaimer,
	readinessState *readiness.State,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) {
	if !appConfig.Scraper.ActiveActive.Enabled {
		return
	}
	_ = startRecoveryLoop(ctx, jobClaimer, readinessState, 5*time.Second, 60*time.Second, logger, func() {
		scraperScheduler.NudgeAllJobs()
		logger.Info("active_active_resumed_nudge")
	})
}

func buildIngestionRuntimeJobClaimer(
	appConfig *config.Config,
	infra *youtubeProducerInfrastructure,
) (poller.JobClaimer, error) {
	jobClaimer, err := polling.BuildJobRunGuardClaimer(infra.cacheService, appConfig.Scraper.ActiveActive)
	if err != nil {
		return nil, fmt.Errorf("build job run guard claimer: %w", err)
	}
	if appConfig.Scraper.ActiveActive.Enabled && jobClaimer == nil {
		return nil, fmt.Errorf("active-active scraper requires job run guard claimer")
	}
	return jobClaimer, nil
}

func buildPollTargetRefresher(
	appConfig *config.Config,
	infra *youtubeProducerInfrastructure,
	deps ingestionRuntimeYouTubeDependencies,
	state ingestionRuntimeYouTubeState,
	logger *slog.Logger,
) *polltarget.Refresher {
	refresher := polltarget.NewRefresher(
		infra.cacheService,
		deps.scraperScheduler,
		deps.pollerRegistrations,
		state.operationalChannels,
		func(ctx context.Context) ([]string, error) {
			return polltarget.LoadAlarmChannelIDs(ctx, infra.postgresService)
		},
		logger,
	).WithTieringDB(infra.postgresService.GetGormDB()).WithOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
		return communityshorts.ResolveOperationalChannelsFromRepository(ctx, infra.memberRepository)
	})
	if appConfig != nil && appConfig.Scraper.ActiveActive.Enabled {
		refresher = refresher.WithInitialJitter(activeActiveInitialJitter(appConfig.Scraper.ActiveActive.InstanceID))
	}
	return refresher
}

func activeActiveInitialJitter(instanceID string) time.Duration {
	trimmed := strings.TrimSpace(instanceID)
	if trimmed == "" {
		return 0
	}
	maxMillis := activeActivePollTargetRefreshMaxJitter.Milliseconds()
	if maxMillis <= 0 {
		return 0
	}
	return time.Duration(crc32.ChecksumIEEE([]byte(trimmed))%uint32(maxMillis)) * time.Millisecond
}

func resolveIngestionSharedScraperClient(scraperConfig config.ScraperConfig, infra *youtubeProducerInfrastructure) *scraper.Client {
	if infra.scraperClient != nil {
		return infra.scraperClient
	}
	return polling.BuildSharedClient(scraperConfig, infra.cacheService, infra.sharedRL)
}

func buildIngestionRuntimeObservationWindowWriter(
	runtimeName string,
	policy communityShortsBigBangPolicy,
	infra *youtubeProducerInfrastructure,
) communityShortsObservationWindowWriter {
	if runtimeName == youtubeProducerRuntimeName && policy.Enabled() {
		return trackingrepo.NewRepository(infra.postgresService.GetGormDB())
	}
	return nil
}
