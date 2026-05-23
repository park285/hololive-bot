package workerapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/hololive-bot/shared-go/pkg/runtime/lifecycle"

	alarmscheduler "github.com/kapu/hololive-alarm-worker/internal/service/alarm/scheduler"
)

const (
	notificationSchedulerRoleEnv = "NOTIFICATION_SCHEDULER_ROLE"
	runtimeRoleBot               = "bot"
	runtimeRoleWorker            = "worker"
	schedulerRoleOff             = "off"
)

type alarmFoundation struct {
	HolodexService *holodex.Service
	ChzzkClient    *chzzk.Client
	TwitchClient   *twitch.Client
	AlarmCRUD      domain.AlarmCRUD
	Outbox         dispatchoutbox.Writer
	Postgres       database.Client
}

func BuildAlarmWorkerRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*AlarmWorkerRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime: build infra module: %w", err)
	}

	foundation, err := buildAlarmFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: alarm foundation: %w", err)
	}

	scheduler, err := buildRuntimeScheduler(appConfig, infra.Cache, foundation, logger, os.Getenv(notificationSchedulerRoleEnv))
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: scheduler: %w", err)
	}

	notificationEgress, err := buildNotificationEgress(appConfig, infra, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: notification egress: %w", err)
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{APIKey: appConfig.Server.APIKey})
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: router: %w", err)
	}

	addr := fmt.Sprintf(":%d", appConfig.Server.Port)
	return &AlarmWorkerRuntime{
		Config:             appConfig,
		Logger:             logger,
		Scheduler:          scheduler,
		NotificationEgress: notificationEgress,
		ConfigSubscriber:   BuildAlarmWorkerConfigSubscriber(infra.Cache, foundation.AlarmCRUD, logger),
		ServerAddr:         addr,
		HTTPServer:         sharedserver.NewH2CServer(addr, router, "hololive-alarm-worker.http"),
		Managed:            lifecycle.NewManaged(infra.Cleanup),
	}, nil
}

func BuildAlarmWorkerConfigSubscriber(
	cacheClient cache.Client,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) *configsub.Subscriber {
	if cacheClient == nil || alarmCRUD == nil {
		return nil
	}

	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			ctx, cancel := context.WithTimeout(context.Background(), constants.RequestTimeout.AdminRequest)
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
	appConfig *config.Config,
	cacheClient cache.Client,
	foundation *alarmFoundation,
	logger *slog.Logger,
	configuredRole string,
) (runtimeAlarmScheduler, error) {
	if !runtimeAllowsAlarmScheduler(runtimeRoleWorker, configuredRole) {
		if logger != nil {
			logger.Info(
				"Alarm runtime scheduler disabled for this runtime",
				slog.String("runtime_role", runtimeRoleWorker),
				slog.String("configured_role", strings.TrimSpace(configuredRole)),
			)
		}
		return nil, nil
	}

	publishConfig, err := loadAlarmDispatchPublishConfig()
	if err != nil {
		return nil, err
	}

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
		parseBoolEnv("ALARM_TWITCH_ENABLED", true),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}

func buildAlarmFoundation(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*alarmFoundation, error) {
	memberData := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := providers.ProvideYouTubeProducerRateLimiter(infra.Cache, logger)
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
	holodexService, err := providers.ProvideHolodexService(appConfig.Holodex.BaseURL, appConfig.Holodex.APIKey, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	chzzkClient := chzzk.NewClientWithConfig(chzzk.ClientConfig{
		ClientID:     appConfig.Chzzk.ClientID,
		ClientSecret: appConfig.Chzzk.ClientSecret,
		Logger:       logger,
	})
	twitchClient := twitch.NewClient(twitch.ClientConfig{
		ClientID:     appConfig.Twitch.ClientID,
		ClientSecret: appConfig.Twitch.ClientSecret,
	}, logger)

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	outboxRepository := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)
	alarmService, err := notification.NewAlarmService(infra.Cache, holodexService, chzzkClient, twitchClient, memberData, alarmRepository, logger, resolved)
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
		Outbox:         outboxRepository,
		Postgres:       infra.Postgres,
	}, nil
}

func loadAlarmDispatchPublishConfig() (queue.PublishConfig, error) {
	mode, err := parseAlarmDispatchPublishMode(os.Getenv("ALARM_DISPATCH_PUBLISH_MODE"))
	if err != nil {
		return queue.PublishConfig{}, err
	}
	if err := validateAlarmDispatchModePair(mode, os.Getenv("ALARM_DISPATCH_CONSUMER_MODE")); err != nil {
		return queue.PublishConfig{}, err
	}
	return queue.PublishConfig{
		Mode:                  mode,
		ShadowFatal:           parseBoolEnv("ALARM_DISPATCH_SHADOW_FATAL", false),
		WakeupEnabled:         parseBoolEnv("ALARM_DISPATCH_WAKEUP_ENABLED", true),
		MaxDeliveriesPerBatch: parsePositiveIntEnv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000),
	}, nil
}

func parseAlarmDispatchPublishMode(raw string) (queue.PublishMode, error) {
	mode := queue.PublishMode(strings.ToLower(strings.TrimSpace(raw)))
	switch mode {
	case "", queue.PublishModeValkeyOnly:
		return queue.PublishModeValkeyOnly, nil
	case queue.PublishModeShadow, queue.PublishModePGFirst:
		return mode, nil
	default:
		return "", fmt.Errorf("ALARM_DISPATCH_PUBLISH_MODE must be valkey_only, shadow, or pg_first")
	}
}
func validateAlarmDispatchModePair(publishMode queue.PublishMode, rawConsumerMode string) error {
	consumerMode := strings.ToLower(strings.TrimSpace(rawConsumerMode))
	if consumerMode == "" {
		if publishMode == queue.PublishModePGFirst {
			return fmt.Errorf("ALARM_DISPATCH_CONSUMER_MODE is required when ALARM_DISPATCH_PUBLISH_MODE=pg_first")
		}
		return nil
	}
	if consumerMode != "valkey" && consumerMode != "pg" {
		return fmt.Errorf("ALARM_DISPATCH_CONSUMER_MODE must be valkey or pg when provided")
	}
	if alarmDispatchModeRequiresPGConsumer(publishMode, consumerMode) {
		return fmt.Errorf("forbidden alarm dispatch mode combination: publisher=pg_first requires consumer=pg")
	}
	if alarmDispatchModeRequiresPGPublisher(publishMode, consumerMode) {
		return fmt.Errorf("forbidden alarm dispatch mode combination: consumer=pg requires publisher=pg_first")
	}
	return nil
}

func alarmDispatchModeRequiresPGConsumer(publishMode queue.PublishMode, consumerMode string) bool {
	return publishMode == queue.PublishModePGFirst && consumerMode != "pg"
}

func alarmDispatchModeRequiresPGPublisher(publishMode queue.PublishMode, consumerMode string) bool {
	return publishMode != queue.PublishModePGFirst && consumerMode == "pg"
}
