package producerruntime

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/config/settings"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	polling2 "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

const activeActivePollTargetRefreshMaxJitter = 2 * time.Second

type ingestionRuntimeYouTubeState struct {
	operationalChannels []communityshorts.OperationalChannel
	pollTargets         polltarget.Targets
	ingestionLease      *ingestionlease.Lease
}

type ingestionRuntimeYouTubeDependencies struct {
	scraperScheduler        *scheduler.Scheduler
	pollerRegistrations     []providers.ChannelPollerRegistration
	pollTargetRefresher     *polltarget.Refresher
	runActiveActiveRecovery func(context.Context)
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

	operationalChannels, err := communityshorts.ResolveOperationalChannels(ctx, infra.memberCache)
	if err != nil {
		return state, fmt.Errorf("resolve community shorts operational channels: %w", err)
	}
	pollTargets, err := polltarget.Resolve(ctx, infra.cacheService, infra.postgresService, operationalChannels, logger)
	if err != nil {
		return state, err
	}

	logger.Info("Resolved YouTube poll targets",
		slog.Int("notification_target_channels", len(pollTargets.NotificationChannelIDs)),
		slog.Int("operational_target_channels", len(pollTargets.OperationalChannelIDs)),
		slog.Int("dropped_alarm_targets", pollTargets.DroppedAlarmTargets),
	)

	state.operationalChannels = operationalChannels
	state.pollTargets = pollTargets

	return state, nil
}

func buildIngestionRuntimeYouTubeDependencies(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *youtubeProducerInfrastructure,
	features ingestionRuntimeFeatures,
	state *ingestionRuntimeYouTubeState,
	readinessState *readiness.State,
) (ingestionRuntimeYouTubeDependencies, error) {
	deps := ingestionRuntimeYouTubeDependencies{}
	budgetCfg := settings.LoadYouTubeProducerGlobalBudgetConfig()
	if readinessState != nil {
		readinessState.SetGlobalBudgetEnabled(budgetCfg.Enabled)
	}
	if !features.youtubeEnabled {
		return deps, nil
	}

	sharedScraperClient := resolveIngestionSharedScraperClient(&appConfig.Scraper, infra)
	jobClaimer, budgetWiring, err := buildIngestionRuntimeCoordination(appConfig, infra, &budgetCfg, readinessState, logger)
	if err != nil {
		return deps, err
	}
	if features.activeActiveEnabled {
		probeReadinessJobClaimer(ctx, jobClaimer, logger)
	}
	liveStatusProvider, err := producerHolodexLiveStatusProvider(infra.holodexService)
	if err != nil {
		return deps, err
	}
	return buildProducerYouTubeDependencies(
		ctx,
		appConfig,
		logger,
		infra,
		state,
		readinessState,
		sharedScraperClient,
		liveStatusProvider,
		jobClaimer,
		budgetWiring,
		deps,
	)
}

func buildProducerYouTubeDependencies(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *youtubeProducerInfrastructure,
	state *ingestionRuntimeYouTubeState,
	readinessState *readiness.State,
	sharedScraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	jobClaimer polling2.JobClaimer,
	budgetWiring polling.GlobalBudgetWiring,
	deps ingestionRuntimeYouTubeDependencies,
) (ingestionRuntimeYouTubeDependencies, error) {
	var err error
	deps.scraperScheduler, deps.pollerRegistrations, err = polling.BuildComponentsWithJobClaimerContext(
		ctx,
		&appConfig.Scraper,
		jobClaimer,
		&budgetWiring,
		infra.postgresService,
		state.pollTargets.NotificationChannelIDs,
		state.pollTargets.OperationalChannelIDs,
		sharedScraperClient,
		liveStatusProvider,
		logger,
	)
	if err != nil {
		return deps, err
	}
	deps.runActiveActiveRecovery = buildActiveActiveRecoveryLoop(appConfig, jobClaimer, readinessState, deps.scraperScheduler, logger)
	deps.pollTargetRefresher = buildPollTargetRefresher(appConfig, infra, deps, state, logger)
	return deps, nil
}

func producerHolodexLiveStatusProvider(holodex *holodexprovider.Service) (pollers.LiveStatusProvider, error) {
	if holodex == nil {
		return nil, fmt.Errorf("youtube producer requires Holodex live status provider")
	}
	return holodex, nil
}

func buildIngestionRuntimeCoordination(
	appConfig *settings.Config,
	infra *youtubeProducerInfrastructure,
	budgetCfg *settings.YouTubeProducerGlobalBudgetConfig,
	readinessState *readiness.State,
	logger *slog.Logger,
) (polling2.JobClaimer, polling.GlobalBudgetWiring, error) {
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
	appConfig *settings.Config,
	infra *youtubeProducerInfrastructure,
	budgetCfg *settings.YouTubeProducerGlobalBudgetConfig,
	readinessState *readiness.State,
	logger *slog.Logger,
) (polling.GlobalBudgetWiring, error) {
	if budgetCfg == nil {
		return polling.GlobalBudgetWiring{}, nil
	}
	if !budgetCfg.Enabled {
		// limiter가 꺼져 있어도 active-active budget 검증은 fleet 인스턴스 수를 요구한다 —
		// zero wiring을 돌려주면 count=0이 되어 producer 부팅이 fail-closed로 죽는다.
		return polling.GlobalBudgetWiring{ActiveInstanceCount: budgetCfg.ActiveInstanceCount}, nil
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
		SourceMaxInflight: map[polling2.BudgetSource]int{
			polling2.BudgetSourceYouTubeScraper:  budgetCfg.YouTubeScraperMaxInflight,
			polling2.BudgetSourceHolodexLive:     budgetCfg.HolodexLiveMaxInflight,
			polling2.BudgetSourceBrowserSnapshot: budgetCfg.BrowserSnapshotMaxInflight,
		},
		ClassMaxInflight: map[polling2.BudgetBurstClass]int{
			polling2.BudgetBurstBackfill: budgetCfg.BackfillMaxInflight,
			polling2.BudgetBurstFallback: budgetCfg.FallbackMaxInflight,
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
		Context: polling2.BudgetContext{
			Namespace:  namespace,
			InstanceID: instanceID,
			Enabled:    true,
		},
		AcquireTimeout:      budgetCfg.AcquireTimeout,
		ActiveInstanceCount: budgetCfg.ActiveInstanceCount,
	}, nil
}

func buildActiveActiveRecoveryLoop(
	appConfig *settings.Config,
	jobClaimer polling2.JobClaimer,
	readinessState *readiness.State,
	scraperScheduler *scheduler.Scheduler,
	logger *slog.Logger,
) func(context.Context) {
	if !appConfig.Scraper.ActiveActive.Enabled {
		return nil
	}
	return func(ctx context.Context) {
		stop := startRecoveryLoop(ctx, jobClaimer, readinessState, 5*time.Second, 60*time.Second, logger, func() {
			scraperScheduler.NudgeAllJobs()
			logger.Info("active_active_resumed_nudge")
		})
		<-ctx.Done()
		stop()
	}
}

func buildIngestionRuntimeJobClaimer(
	appConfig *settings.Config,
	infra *youtubeProducerInfrastructure,
) (polling2.JobClaimer, error) {
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
	appConfig *settings.Config,
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
	).WithTieringDB(infra.postgresService.GetPool()).WithOperationalChannelLoader(func(ctx context.Context) ([]communityshorts.OperationalChannel, error) {
		return communityshorts.ResolveOperationalChannels(ctx, infra.memberCache)
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

func resolveIngestionSharedScraperClient(scraperConfig *settings.ScraperConfig, infra *youtubeProducerInfrastructure) *scraper.Client {
	if infra.scraperClient != nil {
		return infra.scraperClient
	}
	return polling.BuildSharedClient(scraperConfig, infra.cacheService, infra.sharedRL)
}

func postgresPool(infra *youtubeProducerInfrastructure) *pgxpool.Pool {
	if infra == nil || infra.postgresService == nil {
		return nil
	}
	return infra.postgresService.GetPool()
}
