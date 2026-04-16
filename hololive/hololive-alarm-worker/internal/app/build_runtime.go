package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
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
		ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra.Cache, foundation.AlarmCRUD, logger),
		ServerAddr:       addr,
		HttpServer:       sharedserver.NewH2CServer(addr, router, "hololive-alarm-worker.http"),
		Managed:          lifecycle.NewManaged(infra.Cleanup),
	}, nil
}

func BuildAlarmWorkerConfigSubscriber(
	ctx context.Context,
	cacheSvc cache.Client,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) *configsub.Subscriber {
	if cacheSvc == nil || alarmCRUD == nil {
		return nil
	}

	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
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

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		cacheSvc,
		foundation.HolodexService,
		foundation.ChzzkClient,
		foundation.TwitchClient,
		foundation.AlarmCRUD,
		cfg.Notification,
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
	resolved := sharedmodules.ResolvePersistedTargetMinutes(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
	alarmService, err := notification.NewAlarmService(infra.Cache, holodexService, chzzkClient, twitchClient, memberData, alarmRepo, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))
	}

	return &alarmFoundation{
		HolodexService: holodexService,
		ChzzkClient:    chzzkClient,
		TwitchClient:   twitchClient,
		AlarmCRUD:      alarmService,
	}, nil
}
