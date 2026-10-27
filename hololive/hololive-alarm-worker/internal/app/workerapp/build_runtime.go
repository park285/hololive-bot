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

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/httpapi"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	alarmscheduler "github.com/kapu/hololive-alarm-worker/internal/service/alarm/scheduler"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	holodexproviders "github.com/kapu/hololive-shared/pkg/providers/holodex"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	notificationSchedulerRoleEnv = "NOTIFICATION_SCHEDULER_ROLE"
	runtimeRoleWorker            = "worker"
)

type alarmFoundation struct {
	HolodexService *holodexprovider.Service
	AlarmService   *subscriptions.AlarmService
	Outbox         dispatchoutbox.Writer
}

func failAlarmWorkerBuild(infra *sharedmodules.InfraModule, stage string, err error) error {
	if infra != nil && infra.Cleanup != nil {
		infra.Cleanup()
	}

	return fmt.Errorf("build alarm worker runtime: %s: %w", stage, err)
}

func BuildAlarmWorkerRuntime(ctx context.Context, appConfig *workerconfig.RuntimeConfig, logger *slog.Logger) (*workerruntime.AlarmWorkerRuntime, error) {
	ctx, logger, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: appConfig.Valkey, Postgres: appConfig.Postgres, Pool: databaseproviders.RuntimePool{Name: "alarm_worker", ReservedConns: 1}}, logger)
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
	appConfig *workerconfig.RuntimeConfig,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
) (runtime *workerruntime.AlarmWorkerRuntime, err error) {
	foundation, err := buildAlarmFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "alarm foundation", err)
	}

	workerState, err := newAlarmWorkerRegistryState(appConfig.AlarmWorkerProfile, infra.Postgres.GetPool())
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "worker registry", err)
	}

	publishConfig := alarmDispatchPublishConfig(appConfig)

	schedulerResult := buildOptionalRuntimeScheduler(appConfig, infra, foundation, publishConfig, logger)
	if schedulerResult.err != nil {
		return nil, failAlarmWorkerBuild(infra, "scheduler", schedulerResult.err)
	}

	notificationEgress, err := buildNotificationEgress(ctx, appConfig, infra, logger, workerState)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "notification egress", err)
	}

	servers, backgroundRunners, stage, err := buildAlarmWorkerHTTPRuntime(ctx, appConfig, infra, foundation, publishConfig, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, stage, err)
	}

	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		servers.Metrics = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey, workerState.registry)
	}

	runtime = newAlarmWorkerRuntime(appConfig, logger, infra, foundation, alarmWorkerRuntimeParts{
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
	appConfig *workerconfig.RuntimeConfig,
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
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	if runtimeSchedulerDisabled(envutil.String(notificationSchedulerRoleEnv, ""), logger) {
		return optionalRuntimeSchedulerResult{}
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(alarmscheduler.Dependencies{
		Cache:          infra.Cache,
		HolodexService: foundation.HolodexService,
		AlarmState:     foundation.AlarmService,
		Postgres:       infra.Postgres,
		Notification:   appConfig.Notification,
		Outbox:         foundation.Outbox,
		Publish:        publishConfig,
		Logger:         logger,
	})
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build runtime scheduler: new alarm worker runtime scheduler: %w", err)}
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
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) (servers *sharedserver.RuntimeHTTPServers, runners alarmWorkerBackgroundRunners, stage string, err error) {
	readyProbe := newAlarmWorkerReadyProbe(infra)

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		ReadyResponder:         publicReadyHandler(ctx, readyProbe),
		InternalReadyResponder: internalReadyHandler(ctx, readyProbe),
		RegisterRoutes: httpapi.NewInternalRouteRegistrar(
			appConfig.Server.APIKey,
			foundation.AlarmService,
			logger,
		),
	})
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "router", fmt.Errorf("runtime router: %w", err)
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
		postgres    sharedreadiness.Pinger
		cacheClient sharedreadiness.ConnectionChecker
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

// alarmSchedulerEnabled는 값이 없거나 worker일 때만 이 worker가 scheduler를 소유한다고 본다. 허용값 검증은 config가 기동 때 한다.
func alarmSchedulerEnabled(configuredRole string) bool {
	role := strings.ToLower(strings.TrimSpace(configuredRole))

	return role == "" || role == runtimeRoleWorker
}

func runtimeSchedulerDisabled(configuredRole string, logger *slog.Logger) bool {
	if alarmSchedulerEnabled(configuredRole) {
		return false
	}

	if logger != nil {
		logger.Info(
			"Alarm runtime scheduler disabled for this runtime",
			slog.String("runtime_role", runtimeRoleWorker),
			slog.String("configured_role", strings.TrimSpace(configuredRole)),
		)
	}

	return true
}

func buildAlarmFoundation(
	ctx context.Context,
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*alarmFoundation, error) {
	memberData := infra.MemberCache

	holodexService, err := buildAlarmHolodexService(appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmRepository := alarmstore.NewRepository(infra.Postgres, logger)
	outboxRepository := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)

	resolved, err := resolvePersistedTargetMinutes(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve alarm target minutes: %w", err)
	}

	alarmService, err := subscriptions.NewAlarmService(infra.Cache, memberData, alarmRepository, logger, resolved)
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

// buildAlarmHolodexService는 채널 live 상태 조회용 Holodex client를 만든다. 공식 일정 폴백을 쓰지 않으므로 멤버
// identity 색인을 적재하는 scraper 없이 만든다.
func buildAlarmHolodexService(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*holodexprovider.Service, error) {
	holodexService, err := holodexproviders.ProvideHolodexService(&appConfig.Holodex, infra.Cache, nil, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return holodexService, nil
}

func warmAlarmService(ctx context.Context, alarmService *subscriptions.AlarmService, logger *slog.Logger) {
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))

		return
	}
}

// alarmDispatchPublishConfig는 검증을 마친 runtime 설정의 batch 한도와 profile의 wakeup 여부를 발행 설정으로 묶는다.
func alarmDispatchPublishConfig(appConfig *workerconfig.RuntimeConfig) queue.PublishConfig {
	return queue.PublishConfig{
		WakeupEnabled:         appConfig.AlarmWorkerProfile.AlarmDispatch.WakeupEnabled,
		MaxDeliveriesPerBatch: appConfig.AlarmDispatchPublish.MaxDeliveriesPerBatch,
	}
}
