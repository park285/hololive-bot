package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"

	"github.com/kapu/hololive-shared/pkg/service/twitch"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-alarm-worker/internal/readiness"
	alarmscheduler "github.com/kapu/hololive-alarm-worker/internal/service/alarm/scheduler"
	"github.com/kapu/hololive-alarm-worker/internal/service/envconfig"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
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
		return nil, err
	}
	if appConfig == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime: build infra module: %w", err)
	}
	return buildAlarmWorkerRuntimeFromInfra(ctx, appConfig, logger, infra)
}

func buildAlarmWorkerRuntimeFromInfra(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
) (runtime *workerruntime.AlarmWorkerRuntime, err error) {
	foundation, err := buildAlarmFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "alarm foundation", err)
	}
	runtimeOwnsAlarmService := false
	defer func() {
		closeErr := closeAlarmServiceOnBuildFailure(ctx, foundation, runtimeOwnsAlarmService)
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close alarm service after build failure: %w", closeErr))
		}
	}()

	scheduler, err := buildRuntimeScheduler(appConfig, infra.Cache, foundation, logger, envutil.String(notificationSchedulerRoleEnv, ""))
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "scheduler", err)
	}

	notificationEgress, err := buildNotificationEgress(appConfig, infra, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, "notification egress", err)
	}

	servers, backgroundRunners, stage, err := buildAlarmWorkerHTTPRuntime(ctx, appConfig, infra, foundation, logger)
	if err != nil {
		return nil, failAlarmWorkerBuild(infra, stage, err)
	}

	runtime = &workerruntime.AlarmWorkerRuntime{
		Config:               appConfig,
		Logger:               logger,
		Scheduler:            scheduler,
		NotificationEgress:   notificationEgress,
		CelebrationRunner:    backgroundRunners.celebration,
		BirthdayStreamRunner: backgroundRunners.birthdayStream,
		ConfigSubscriber:     BuildAlarmWorkerConfigSubscriber(ctx, infra.Cache, foundation.AlarmCRUD, logger),
		ServerAddr:           servers.Addr(),
		HTTPServers:          servers,
		AlarmService:         foundation.AlarmService,
		Managed:              lifecycle.NewManaged(infra.Cleanup),
	}
	runtimeOwnsAlarmService = true
	return runtime, nil
}

func closeAlarmServiceOnBuildFailure(ctx context.Context, foundation *alarmFoundation, owned bool) error {
	if owned || foundation == nil || foundation.AlarmService == nil {
		return nil
	}
	return foundation.AlarmService.Close(ctx)
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
		return nil, alarmWorkerBackgroundRunners{}, "router", err
	}

	publishConfig := loadAlarmDispatchPublishConfig()
	runners = alarmWorkerBackgroundRunners{
		celebration:    buildCelebrationRunnerScheduler(infra, foundation, publishConfig, logger),
		birthdayStream: buildBirthdayStreamRunnerScheduler(infra, foundation, publishConfig, logger),
	}

	servers, err = sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, "hololive-alarm-worker.http",
		sharedserver.LocalPlaneTraceFilter)
	if err != nil {
		return nil, alarmWorkerBackgroundRunners{}, "http servers", err
	}

	return servers, runners, "", nil
}

func newAlarmWorkerReadyProbe(infra *sharedmodules.InfraModule) *sharedreadiness.Probe {
	var postgres database.Client
	var cacheClient cache.Client
	if infra != nil {
		postgres = infra.Postgres
		cacheClient = infra.Cache
	}
	return sharedreadiness.NewProbe("alarm-worker",
		sharedreadiness.PostgresCheck(postgres),
		sharedreadiness.ValkeyCheck(cacheClient),
		readiness.BoolEnvNotFalseCheck("delivery_dispatcher_enabled", "DELIVERY_DISPATCHER_ENABLED", true),
		readiness.BoolEnvNotFalseCheck("alarm_dispatch_consumer_enabled", "ALARM_DISPATCH_CONSUMER_ENABLED", true),
		readiness.ExplicitTrueBoolEnvCheck("youtube_outbox_dispatcher_enabled", "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"),
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
			ctx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.AdminRequest)
			defer cancel()

			targets := alarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
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

func buildRuntimeScheduler(
	appConfig *settings.Config,
	cacheClient cache.Client,
	foundation *alarmFoundation,
	logger *slog.Logger,
	configuredRole string,
) (workerruntime.Scheduler, error) {
	if err := validateRuntimeSchedulerInputs(appConfig, foundation); err != nil {
		return nil, err
	}
	if runtimeSchedulerDisabled(runtimeRoleWorker, configuredRole, logger) {
		return nil, nil
	}

	publishConfig := loadAlarmDispatchPublishConfig()

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		cacheClient,
		foundation.HolodexService,
		foundation.ChzzkClient,
		foundation.TwitchClient,
		foundation.AlarmCRUD,
		foundation.Postgres,
		appConfig.Notification,
		foundation.Outbox,
		publishConfig,
		envutil.Bool("ALARM_TWITCH_ENABLED", true),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}

func validateRuntimeSchedulerInputs(appConfig *settings.Config, foundation *alarmFoundation) error {
	if appConfig == nil {
		return fmt.Errorf("config is required")
	}
	if foundation == nil {
		return fmt.Errorf("alarm foundation is required")
	}
	return nil
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
	sharedRL, err := providers.ProvideYouTubeProducerRateLimiterWithConfig(&appConfig.YouTube, infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.Cache,
		memberData,
		scraper.ProxyConfig{Enabled: appConfig.Scraper.ProxyEnabled, URL: appConfig.Scraper.ProxyURL},
		sharedRL,
		logger,
	)
	holodexService, err := providers.ProvideHolodexServiceWithConfig(&appConfig.Holodex, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	chzzkClient := chzzk.NewClientWithConfig(&chzzk.ClientConfig{
		ClientID:     appConfig.Chzzk.ClientID,
		ClientSecret: appConfig.Chzzk.ClientSecret,
		Logger:       logger,
	})
	twitchClient := twitch.NewClient(&twitch.ClientConfig{
		ClientID:     appConfig.Twitch.ClientID,
		ClientSecret: appConfig.Twitch.ClientSecret,
	}, logger)

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	outboxRepository := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)
	alarmService, err := alarmservice.NewAlarmService(infra.Cache, holodexService, chzzkClient, twitchClient, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))
	} else if err := alarmService.SyncPlatformMappings(ctx); err != nil {
		logger.Warn("Failed to sync platform alarm mappings", slog.Any("error", err))
	}

	return &alarmFoundation{
		HolodexService: holodexService,
		ChzzkClient:    chzzkClient,
		TwitchClient:   twitchClient,
		AlarmCRUD:      alarmService,
		AlarmService:   alarmService,
		Outbox:         outboxRepository,
		Postgres:       infra.Postgres,
	}, nil
}

func loadAlarmDispatchPublishConfig() queue.PublishConfig {
	return queue.PublishConfig{
		WakeupEnabled:         envutil.Bool("ALARM_DISPATCH_WAKEUP_ENABLED", true),
		MaxDeliveriesPerBatch: envconfig.ParsePositiveInt("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000),
	}
}
