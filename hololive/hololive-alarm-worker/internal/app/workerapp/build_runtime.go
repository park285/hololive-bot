package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-alarm-worker/internal/readiness"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	alarmscheduler "github.com/kapu/hololive-alarm-worker/internal/service/alarm/scheduler"
	"github.com/kapu/hololive-alarm-worker/internal/service/envconfig"
	"github.com/kapu/hololive-alarm-worker/internal/service/notification/alarmservice"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	notificationSchedulerRoleEnv = "NOTIFICATION_SCHEDULER_ROLE"
	runtimeRoleBot               = "bot"
	runtimeRoleWorker            = "worker"
	schedulerRoleOff             = "off"
)

type alarmFoundation struct {
	HolodexService *holodexprovider.Service
	AlarmService   *alarmservice.AlarmService
	Outbox         dispatchoutbox.Writer
}

func failAlarmWorkerBuild(infra *sharedmodules.InfraModule, stage string, err error) error {
	if infra != nil && infra.Cleanup != nil {
		infra.Cleanup()
	}

	return fmt.Errorf("build alarm worker runtime: %s: %w", stage, err)
}

func BuildAlarmWorkerRuntime(ctx context.Context, appConfig *alarmworker.RuntimeConfig, logger *slog.Logger) (*workerruntime.AlarmWorkerRuntime, error) {
	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig.Config, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime: build infra module: %w", err)
	}

	out, err := buildAlarmWorkerRuntimeFromInfra(ctx, appConfig, logger, infra)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime from infra: %w", err)
	}

	return out, nil
}

func buildAlarmWorkerRuntimeFromInfra(
	ctx context.Context,
	appConfig *alarmworker.RuntimeConfig,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
) (runtime *workerruntime.AlarmWorkerRuntime, err error) {
	foundation, err := buildAlarmFoundation(ctx, appConfig.Config, infra, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "alarm foundation", err)
	}

	workerState, err := newAlarmWorkerRegistryState(appConfig.AlarmWorkerProfile, infra.Postgres.GetPool())
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "worker registry", err)
	}

	schedulerResult := buildOptionalRuntimeScheduler(appConfig.Config, infra, foundation, logger)
	if schedulerResult.err != nil {
		return nil, failAlarmWorkerBuild(infra, "scheduler", schedulerResult.err)
	}

	notificationEgress, err := buildNotificationEgress(ctx, appConfig, infra, logger, workerState)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "notification egress", err)
	}

	servers, backgroundRunners, stage, err := buildAlarmWorkerHTTPRuntime(ctx, appConfig.Config, infra, foundation, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, stage, err)
	}

	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		servers.Metrics = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey, workerState.registry)
	}

	runtime = newAlarmWorkerRuntime(appConfig.Config, logger, infra, foundation, alarmWorkerRuntimeParts{
		scheduler:          schedulerResult.scheduler,
		notificationEgress: notificationEgress,
		servers:            servers,
		backgroundRunners:  backgroundRunners,
		workerState:        workerState,
	})

	return runtime, nil
}

type alarmWorkerRuntimeParts struct {
	scheduler          workerruntime.Scheduler
	notificationEgress workerruntime.Scheduler
	servers            *sharedserver.RuntimeHTTPServers
	backgroundRunners  alarmWorkerBackgroundRunners
	workerState        *alarmWorkerRegistryState
}

func newAlarmWorkerRuntime(
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	parts alarmWorkerRuntimeParts,
) *workerruntime.AlarmWorkerRuntime {
	return &workerruntime.AlarmWorkerRuntime{
		Config:               appConfig,
		Logger:               logger,
		Scheduler:            parts.scheduler,
		NotificationEgress:   parts.notificationEgress,
		CelebrationRunner:    parts.backgroundRunners.celebration,
		BirthdayStreamRunner: parts.backgroundRunners.birthdayStream,
		XSpacesRunner:        parts.backgroundRunners.xSpaces,
		ServerAddr:           parts.servers.Addr(),
		HTTPServers:          parts.servers,
		WorkerObservability:  parts.workerState,
		Managed:              lifecycle.NewManaged(stopHolodexRetriesBeforeCleanup(foundation.HolodexService, infra.Cleanup)),
	}
}

// stopHolodexRetriesBeforeCleanup은 Holodex 캐시 워밍 재시도를 멈춘 뒤 infra를 닫는다.
// 재시도는 예약한 요청 ctx의 취소와 분리되어 scheduler의 Stop만 끝낼 수 있다(holodexprovider retry_scheduler).
// Valkey·PG infra를 먼저 닫으면 대기 중이거나 실행 중인 재시도가 닫힌 client를 쓴다.
func stopHolodexRetriesBeforeCleanup(holodex interface{ Stop() }, cleanup func()) func() {
	return func() {
		holodex.Stop()
		cleanup()
	}
}

type optionalRuntimeSchedulerResult struct {
	scheduler workerruntime.Scheduler
	err       error
}

func buildOptionalRuntimeScheduler(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	if runtimeSchedulerDisabled(runtimeRoleWorker, envutil.String(notificationSchedulerRoleEnv, ""), logger) {
		return optionalRuntimeSchedulerResult{}
	}

	scheduler, err := buildRuntimeScheduler(appConfig, infra, foundation, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build runtime scheduler: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: scheduler}
}

type alarmWorkerBackgroundRunners struct {
	celebration    workerruntime.Scheduler
	birthdayStream workerruntime.Scheduler
	xSpaces        workerruntime.Scheduler
}

func buildAlarmWorkerHTTPRuntime(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	logger *slog.Logger,
) (servers *sharedserver.RuntimeHTTPServers, runners alarmWorkerBackgroundRunners, stage string, err error) {
	readyProbe := newAlarmWorkerReadyProbe(infra)

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		ReadyResponder:         readiness.PublicGinHandler(ctx, readyProbe),
		InternalReadyResponder: readiness.InternalGinHandler(ctx, readyProbe),
		RegisterRoutes: sharedalarm.NewInternalRouteRegistrar(
			appConfig.Server.APIKey,
			foundation.AlarmService,
			logger,
		),
	})
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "router", fmt.Errorf("runtime router: %w", err)
	}

	publishConfig, err := loadAlarmDispatchPublishConfig(appConfig.AlarmWorkerProfile)
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "publish config", err
	}

	xSpaces := buildXSpacesRunner(infra, foundation, publishConfig, logger)

	if xSpaces.err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "X spaces", xSpaces.err
	}

	celebrationRunner := buildCelebrationRunnerScheduler(infra, foundation, publishConfig, logger)
	if celebrationRunner.err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "celebration runner", celebrationRunner.err
	}

	birthdayStreamRunner := buildBirthdayStreamRunnerScheduler(infra, foundation, publishConfig, logger)
	if birthdayStreamRunner.err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "birthday stream runner", birthdayStreamRunner.err
	}

	runners = alarmWorkerBackgroundRunners{
		celebration:    celebrationRunner.scheduler,
		birthdayStream: birthdayStreamRunner.scheduler,
		xSpaces:        xSpaces.scheduler,
	}

	servers, err = sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, "hololive-alarm-worker.http",
		nil, sharedserver.LocalPlaneTraceFilter)
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "http servers", fmt.Errorf("runtime HTTP servers: %w", err)
	}

	return servers, runners, "", nil
}

func newAlarmWorkerReadyProbe(infra *sharedmodules.InfraModule) *sharedreadiness.Probe {
	var (
		postgres    database.Client
		cacheClient cache.Client
	)

	if infra != nil {
		postgres = infra.Postgres
		cacheClient = infra.Cache
	}

	return sharedreadiness.NewProbe("alarm-worker",
		sharedreadiness.PostgresCheck(postgres),
		sharedreadiness.ValkeyCheck(cacheClient),
	)
}

func runtimeAllowsAlarmScheduler(runtimeRole, configuredRole string) bool {
	role := strings.ToLower(strings.TrimSpace(configuredRole))
	if role == "" {
		role = strings.ToLower(strings.TrimSpace(runtimeRole))
	}

	switch role {
	case schedulerRoleOff:
		return false
	case runtimeRoleBot, runtimeRoleWorker:
		return strings.ToLower(strings.TrimSpace(runtimeRole)) == role
	default:
		return false
	}
}

func buildRuntimeScheduler(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	logger *slog.Logger,
) (workerruntime.Scheduler, error) {
	if appConfig == nil {
		return nil, errors.New("validate runtime scheduler inputs: config is required")
	}

	if foundation == nil {
		return nil, errors.New("validate runtime scheduler inputs: alarm foundation is required")
	}

	if foundation.AlarmService == nil {
		return nil, errors.New("validate runtime scheduler inputs: alarm service is required")
	}

	if infra == nil {
		return nil, errors.New("validate runtime scheduler inputs: infrastructure is required")
	}

	if appConfig.AlarmWorkerProfile == nil {
		return nil, errors.New("validate runtime scheduler inputs: alarm worker profile is required")
	}

	publishConfig, err := loadAlarmDispatchPublishConfig(appConfig.AlarmWorkerProfile)
	if err != nil {
		return nil, err
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(alarmscheduler.Dependencies{
		Cache:          infra.Cache,
		HolodexService: foundation.HolodexService,
		AlarmCRUD:      foundation.AlarmService,
		Postgres:       infra.Postgres,
		Notification:   appConfig.Notification,
		Outbox:         foundation.Outbox,
		Publish:        publishConfig,
		Logger:         logger,
	})
	if err != nil {
		return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}

func runtimeSchedulerDisabled(runtimeRole, configuredRole string, logger *slog.Logger) bool {
	if runtimeAllowsAlarmScheduler(runtimeRole, configuredRole) {
		return false
	}

	if logger != nil {
		logger.Info(
			"Alarm runtime scheduler disabled for this runtime",
			slog.String("runtime_role", runtimeRole),
			slog.String("configured_role", strings.TrimSpace(configuredRole)),
		)
	}

	return true
}

func buildAlarmFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*alarmFoundation, error) {
	memberData := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	holodexService, err := buildAlarmHolodexService(appConfig, infra, memberData, logger)
	if err != nil {
		return nil, err
	}

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	outboxRepository := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)

	resolved, err := sharedmodules.ResolvePersistedTargetMinutes(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve alarm target minutes: %w", err)
	}

	alarmService, err := alarmservice.NewAlarmService(infra.Cache, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}

	warmAlarmService(ctx, alarmService, logger)

	return &alarmFoundation{
		HolodexService: holodexService,
		AlarmService:   alarmService,
		Outbox:         outboxRepository,
	}, nil
}

// buildAlarmHolodexService는 runtime이 읽은 YouTube·Holodex 설정으로 스크레이퍼와 Holodex 서비스를 만든다.
func buildAlarmHolodexService(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	memberData domain.MemberDataProvider,
	logger *slog.Logger,
) (*holodexprovider.Service, error) {
	sharedRL, err := providers.ProvideYouTubeRateLimiterWithConfig(&appConfig.YouTube, infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService, err := providers.ProvideScraperServiceWithOfficialSchedule(
		memberData,
		appConfig.YouTube,
		sharedRL,
		logger,
		appConfig.OfficialScheduleRuntime(),
	)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	holodexService, err := providers.ProvideHolodexServiceWithConfig(&appConfig.Holodex, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return holodexService, nil
}

func warmAlarmService(ctx context.Context, alarmService *alarmservice.AlarmService, logger *slog.Logger) {
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))

		return
	}
}

// 잘못된 batch 한도를 기본값 1000으로 바꾸지 않고 기동 오류로 드러낸다(holo-alarm-worker-envconfig-silent-defaults).
func loadAlarmDispatchPublishConfig(profile *settings.AlarmWorkerProfile) (queue.PublishConfig, error) {
	maxDeliveries, err := envconfig.ParsePositiveInt("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000)
	if err != nil {
		return queue.PublishConfig{}, fmt.Errorf("alarm dispatch publish config: %w", err)
	}

	return queue.PublishConfig{
		WakeupEnabled:         profile.AlarmDispatch.WakeupEnabled,
		MaxDeliveriesPerBatch: maxDeliveries,
	}, nil
}
