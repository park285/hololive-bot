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
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const (
	notificationSchedulerRoleEnv = "NOTIFICATION_SCHEDULER_ROLE"
	runtimeRoleBot               = "bot"
	runtimeRoleWorker            = "worker"
	schedulerRoleOff             = "off"
)

type alarmFoundation struct {
	HolodexService *holodexprovider.Service
	ChzzkClient    *chzzk.Client
	TwitchClient   *twitch.Client
	AlarmCRUD      domain.AlarmCRUD
	AlarmService   *alarmservice.AlarmService
	Outbox         dispatchoutbox.Writer
	Postgres       database.Client
}

func failAlarmWorkerBuild(infra *sharedmodules.InfraModule, stage string, err error) error {
	if infra != nil && infra.Cleanup != nil {
		infra.Cleanup()
	}

	return fmt.Errorf("build alarm worker runtime: %s: %w", stage, err)
}

func BuildAlarmWorkerRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*workerruntime.AlarmWorkerRuntime, error) {
	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
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
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
) (runtime *workerruntime.AlarmWorkerRuntime, err error) {
	foundation, err := buildAlarmFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, failAlarmWorkerBuild(infra, "alarm foundation", err)
	}

	runtimeOwnsAlarmService := false

	defer func() {
		closeErr := closeAlarmServiceOnBuildFailure(ctx, foundation, runtimeOwnsAlarmService)
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close alarm service after build failure: %w", closeErr))
		}
	}()

	workerState, err := newAlarmWorkerRegistryState(appConfig.AlarmWorkerProfile, infra.Postgres.GetPool())
	if err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, failAlarmWorkerBuild(infra, "worker registry", err)
	}

	schedulerResult := buildOptionalRuntimeScheduler(appConfig, infra.Cache, foundation, logger)
	if schedulerResult.err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, failAlarmWorkerBuild(infra, "scheduler", schedulerResult.err)
	}

	notificationEgress, err := buildNotificationEgress(ctx, appConfig, infra, logger, workerState)
	if err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, failAlarmWorkerBuild(infra, "notification egress", err)
	}

	servers, backgroundRunners, stage, err := buildAlarmWorkerHTTPRuntime(ctx, appConfig, infra, foundation, logger)
	if err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, failAlarmWorkerBuild(infra, stage, err)
	}

	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		servers.Metrics = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey, workerState.registry)
	}

	runtime = newAlarmWorkerRuntime(ctx, appConfig, logger, infra, foundation, alarmWorkerRuntimeParts{
		scheduler:          schedulerResult.scheduler,
		notificationEgress: notificationEgress,
		servers:            servers,
		backgroundRunners:  backgroundRunners,
		workerState:        workerState,
	})
	runtimeOwnsAlarmService = true

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
	ctx context.Context,
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
		ConfigSubscriber:     BuildAlarmWorkerConfigSubscriber(ctx, infra.Cache, foundation.AlarmCRUD, logger),
		ServerAddr:           parts.servers.Addr(),
		HTTPServers:          parts.servers,
		AlarmService:         foundation.AlarmService,
		WorkerObservability:  parts.workerState,
		Managed:              lifecycle.NewManaged(infra.Cleanup),
	}
}

type optionalRuntimeSchedulerResult struct {
	scheduler workerruntime.Scheduler
	err       error
}

func buildOptionalRuntimeScheduler(
	appConfig *settings.Config,
	cacheClient cache.Client,
	foundation *alarmFoundation,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	if runtimeSchedulerDisabled(runtimeRoleWorker, envutil.String(notificationSchedulerRoleEnv, ""), logger) {
		return optionalRuntimeSchedulerResult{}
	}

	scheduler, err := buildRuntimeScheduler(appConfig, cacheClient, foundation, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build runtime scheduler: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: scheduler}
}

func closeAlarmServiceOnBuildFailure(ctx context.Context, foundation *alarmFoundation, owned bool) error {
	if owned || foundation == nil || foundation.AlarmService == nil {
		return nil
	}

	if err := foundation.AlarmService.Close(ctx); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

type alarmWorkerBackgroundRunners struct {
	celebration    workerruntime.Scheduler
	birthdayStream workerruntime.Scheduler
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
			foundation.AlarmCRUD,
			logger,
		),
	})
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "router", fmt.Errorf("runtime router: %w", err)
	}

	publishConfig := loadAlarmDispatchPublishConfig(appConfig.AlarmWorkerProfile)

	runners = alarmWorkerBackgroundRunners{
		celebration:    buildCelebrationRunnerScheduler(infra, foundation, publishConfig, logger),
		birthdayStream: buildBirthdayStreamRunnerScheduler(infra, foundation, publishConfig, logger),
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

func BuildAlarmWorkerConfigSubscriber(
	ctx context.Context,
	cacheClient cache.Client,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) *configsub.Subscriber {
	if cacheClient == nil || alarmCRUD == nil {
		return nil
	}

	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			applyCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.AdminRequest)
			defer cancel()

			targets := alarmCRUD.UpdateAlarmAdvanceMinutes(applyCtx, payload.Minutes)

			if logger != nil {
				logger.Info(
					"Alarm worker applied alarm advance minutes via pub/sub",
					slog.Int("minutes", payload.Minutes),
					slog.Any("targets", targets),
				)
			}
		},
	})

	return configsub.New(cacheClient.GetClient(), applyFn, logger)
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
