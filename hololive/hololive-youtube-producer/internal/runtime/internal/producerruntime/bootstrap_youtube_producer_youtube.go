package producerruntime

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

const activeActivePollTargetRefreshMaxJitter = 2 * time.Second

type ingestionRuntimeYouTubeState struct {
	operationalChannels []communityShortsOperationalChannel
	pollTargets         polltarget.Targets
	ingestionLease      *ingestionlease.Lease
}

type ingestionRuntimeYouTubeDependencies struct {
	scraperScheduler    *poller.Scheduler
	pollerRegistrations []providers.ChannelPollerRegistration
	pollTargetRefresher *polltarget.Refresher
}

func resolveIngestionRuntimeYouTubeState(
	ctx context.Context,
	logger *slog.Logger,
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

	return state, nil
}

func buildIngestionRuntimeYouTubeDependencies(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	infra *youtubeProducerInfrastructure,
	enabled bool,
	state *ingestionRuntimeYouTubeState,
	readinessState *readiness.State,
) (ingestionRuntimeYouTubeDependencies, error) {
	deps := ingestionRuntimeYouTubeDependencies{}
	budgetCfg := config.LoadYouTubeProducerGlobalBudgetConfig()
	if readinessState != nil {
		readinessState.SetGlobalBudgetEnabled(budgetCfg.Enabled)
	}
	if !enabled {
		return deps, nil
	}

	sharedScraperClient := resolveIngestionSharedScraperClient(&appConfig.Scraper, infra)
	jobClaimer, budgetWiring, err := buildIngestionRuntimeCoordination(appConfig, infra, &budgetCfg, readinessState, logger)
	if err != nil {
		return deps, err
	}
	if appConfig.Scraper.ActiveActive.Enabled {
		probeReadinessJobClaimer(ctx, jobClaimer, logger)
	}
	deps.scraperScheduler, deps.pollerRegistrations, err = polling.BuildComponentsWithJobClaimerContext(
		ctx,
		&appConfig.Scraper,
		jobClaimer,
		&budgetWiring,
		infra.postgresService,
		state.pollTargets.NotificationChannelIDs,
		state.pollTargets.StatsChannelIDs,
		sharedScraperClient,
		infra.holodexService,
		logger,
	)
	if err != nil {
		return deps, err
	}
	startActiveActiveRecoveryLoopIfEnabled(ctx, appConfig, jobClaimer, readinessState, deps.scraperScheduler, logger)
	deps.pollTargetRefresher = buildPollTargetRefresher(appConfig, infra, deps, state, logger)
	return deps, nil
}

func buildIngestionRuntimeCoordination(
	appConfig *config.Config,
	infra *youtubeProducerInfrastructure,
	budgetCfg *config.YouTubeProducerGlobalBudgetConfig,
	readinessState *readiness.State,
	logger *slog.Logger,
) (poller.JobClaimer, polling.GlobalBudgetWiring, error) {
	jobClaimer, err := buildIngestionRuntimeJobClaimer(appConfig, infra)
	if err != nil {
		return nil, polling.GlobalBudgetWiring{}, err
	}
	jobClaimer = newReadinessReportingJobClaimer(jobClaimer, readinessState)
	budgetWiring, err := buildIngestionRuntimeGlobalBudgetWiring(appConfig, infra, budgetCfg, readinessState, logger)
	if err != nil {
		return nil, polling.GlobalBudgetWiring{}, err
	}
	budgetWiring.BudgetRPM = youtubeProducerBudgetRPM(appConfig.YouTube.ProducerRequestInterval)
	return jobClaimer, budgetWiring, nil
}

func youtubeProducerBudgetRPM(requestInterval time.Duration) float64 {
	if requestInterval <= 0 {
		return 0
	}
	return 60.0 / requestInterval.Seconds()
}

func buildIngestionRuntimeGlobalBudgetWiring(
	appConfig *config.Config,
	infra *youtubeProducerInfrastructure,
	budgetCfg *config.YouTubeProducerGlobalBudgetConfig,
	readinessState *readiness.State,
	logger *slog.Logger,
) (polling.GlobalBudgetWiring, error) {
	if budgetCfg == nil || !budgetCfg.Enabled {
		return polling.GlobalBudgetWiring{}, nil
	}
	if budgetCfg.WindowCheckEnabled && logger != nil {
		logger.Warn("budget_window_check_not_implemented",
			slog.String("env", "YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED"),
			slog.String("effect", "ignored_in_phase1"),
		)
	}
	namespace := strings.TrimSpace(appConfig.Scraper.ActiveActive.Namespace)
	if namespace == "" {
		return polling.GlobalBudgetWiring{}, fmt.Errorf("build global budget limiter: active-active namespace must not be empty")
	}
	instanceID := strings.TrimSpace(appConfig.Scraper.ActiveActive.InstanceID)
	limiter, err := polling.NewGlobalBudgetLimiter(infra.cacheService, polling.GlobalBudgetLimiterConfig{
		Namespace:  namespace,
		InstanceID: instanceID,
		SourceMaxInflight: map[poller.BudgetSource]int{
			poller.BudgetSourceYouTubeScraper:  budgetCfg.YouTubeScraperMaxInflight,
			poller.BudgetSourceHolodexLive:     budgetCfg.HolodexLiveMaxInflight,
			poller.BudgetSourceBrowserSnapshot: budgetCfg.BrowserSnapshotMaxInflight,
		},
		ClassMaxInflight: map[poller.BudgetBurstClass]int{
			poller.BudgetBurstBackfill: budgetCfg.BackfillMaxInflight,
			poller.BudgetBurstFallback: budgetCfg.FallbackMaxInflight,
		},
		WindowCheckEnabled: budgetCfg.WindowCheckEnabled,
		CleanupLimit:       budgetCfg.CleanupLimit,
	})
	if err != nil {
		return polling.GlobalBudgetWiring{}, fmt.Errorf("build global budget limiter: %w", err)
	}
	limiter = newReadinessReportingBudgetLimiter(limiter, readinessState)
	return polling.GlobalBudgetWiring{
		Limiter: limiter,
		Context: poller.BudgetContext{
			Namespace:  namespace,
			InstanceID: instanceID,
			Enabled:    true,
		},
		AcquireTimeout:      budgetCfg.AcquireTimeout,
		ActiveInstanceCount: budgetCfg.ActiveInstanceCount,
	}, nil
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
	state *ingestionRuntimeYouTubeState,
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
	).WithTieringDB(infra.postgresService.GetPool()).WithOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
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
	if maxMillis > math.MaxUint32 {
		maxMillis = math.MaxUint32
	}
	return time.Duration(crc32.ChecksumIEEE([]byte(trimmed))%uint32(maxMillis)) * time.Millisecond
}

func resolveIngestionSharedScraperClient(scraperConfig *config.ScraperConfig, infra *youtubeProducerInfrastructure) *scraper.Client {
	if infra.scraperClient != nil {
		return infra.scraperClient
	}
	return polling.BuildSharedClient(scraperConfig, infra.cacheService, infra.sharedRL)
}
