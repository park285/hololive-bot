package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

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
}

func BuildAlarmWorkerRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AlarmWorkerRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime: build infra module: %w", err)
	}

	foundation, err := buildAlarmFoundation(ctx, cfg, infra, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: alarm foundation: %w", err)
	}

	scheduler, err := buildRuntimeScheduler(cfg, infra.Cache, foundation, logger, os.Getenv(notificationSchedulerRoleEnv))
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: scheduler: %w", err)
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{APIKey: cfg.Server.APIKey})
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: router: %w", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	return &AlarmWorkerRuntime{
		Config:           cfg,
		Logger:           logger,
		Scheduler:        scheduler,
		ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(infra.Cache, foundation.AlarmCRUD, logger),
		ServerAddr:       addr,
		HttpServer:       sharedserver.NewH2CServer(addr, router, "hololive-alarm-worker.http"),
		Managed:          lifecycle.NewManaged(infra.Cleanup),
	}, nil
}

func BuildAlarmWorkerConfigSubscriber(
	cacheSvc cache.Client,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) *configsub.Subscriber {
	if cacheSvc == nil || alarmCRUD == nil {
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

	return configsub.New(cacheSvc.GetClient(), applyFn, logger)
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
	cfg *config.Config,
	cacheSvc cache.Client,
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
		cacheSvc,
		foundation.HolodexService,
		foundation.ChzzkClient,
		foundation.TwitchClient,
		foundation.AlarmCRUD,
		cfg.Notification,
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
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*alarmFoundation, error) {
	memberData := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.Cache,
		memberData,
		scraper.ProxyConfig{Enabled: cfg.Scraper.ProxyEnabled, URL: cfg.Scraper.ProxyURL},
		sharedRL,
		logger,
	)
	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, cfg.Holodex.APIKey, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	chzzkClient := chzzk.NewClientWithConfig(chzzk.ClientConfig{
		ClientID:     cfg.Chzzk.ClientID,
		ClientSecret: cfg.Chzzk.ClientSecret,
		Logger:       logger,
	})
	twitchClient := twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.Twitch.ClientID,
		ClientSecret: cfg.Twitch.ClientSecret,
	}, logger)

	alarmRepo := sharedalarm.NewRepository(infra.Postgres, logger)
	outboxRepo := dispatchoutbox.NewPgxRepository(infra.Postgres)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
	alarmService, err := notification.NewAlarmService(infra.Cache, holodexService, chzzkClient, twitchClient, memberData, alarmRepo, logger, resolved)
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
		Outbox:         outboxRepo,
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

func parseBoolEnv(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func parsePositiveIntEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}
	return value
}
