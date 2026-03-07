package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
	triggerclient "github.com/kapu/hololive-kakao-bot-go/internal/service/trigger"
)

type botAdminServerDependencies struct {
	domainHandlers *server.DomainAPIHandlers
	authHandler    *server.AuthHandler
	cache          cache.Client
}

func buildBotAdminServerDependencies(
	ctx context.Context,
	cfg *config.Config,
	deps botAdminRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) (*botAdminServerDependencies, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: config is nil")
	}
	if deps.cache == nil || deps.postgres == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: admin dependency view is incomplete")
	}

	authService, err := buildBotAdminAuthService(ctx, cfg, deps, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot admin server dependencies: %w", err)
	}

	settingsComponents := buildBotAdminSettingsComponents(cfg, deps, scraperScheduler, logger)
	systemCollector := buildBotAdminSystemCollector(cfg)
	domainHandlers := buildBotAdminAPIHandlers(
		deps,
		scraperScheduler,
		settingsComponents.settingsApplier,
		settingsComponents.majorEventTriggerClient,
		systemCollector,
		logger,
	)

	return &botAdminServerDependencies{
		domainHandlers: domainHandlers,
		authHandler:    server.NewAuthHandler(authService, logger),
		cache:          deps.cache,
	}, nil
}

type botAdminSettingsComponents struct {
	settingsApplier         sharedsettings.SettingsApplier
	majorEventTriggerClient *triggerclient.Client
}

func buildBotAdminAuthService(
	ctx context.Context,
	cfg *config.Config,
	deps botAdminRuntimeDependencies,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = cfg.Postgres.AutoPrepareSchema

	authService, err := authsvc.NewService(
		ctx,
		deps.postgres.GetGormDB(),
		deps.cache,
		logger,
		authCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("create auth service: %w", err)
	}

	return authService, nil
}

func buildBotAdminSettingsComponents(
	cfg *config.Config,
	deps botAdminRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) botAdminSettingsComponents {
	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(
		deps.youtubeService,
		deps.holodexService,
		scraperScheduler,
		deps.alarmCRUD,
	)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	var majorEventTriggerClient *triggerclient.Client
	if strings.TrimSpace(cfg.LLMSchedulerURL) != "" {
		majorEventTriggerClient = triggerclient.NewClient(cfg.LLMSchedulerURL, cfg.Server.APIKey, logger)
		settingsApplier = newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger)
	} else {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)
	}

	return botAdminSettingsComponents{
		settingsApplier:         settingsApplier,
		majorEventTriggerClient: majorEventTriggerClient,
	}
}

func buildBotAdminSystemCollector(cfg *config.Config) *system.Collector {
	return system.NewCollector(
		[]system.ServiceEndpoint{
			{Name: "llm-scheduler", URL: cfg.Services.LLMSchedulerHealthURL},
			{Name: "twentyq", URL: cfg.Services.GameBotTwentyQHealthURL},
			{Name: "turtlesoup", URL: cfg.Services.GameBotTurtleHealthURL},
		},
		cfg.Telemetry.Enabled,
	)
}

func buildBotAdminAPIHandlers(
	deps botAdminRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	settingsApplier sharedsettings.SettingsApplier,
	majorEventTriggerClient *triggerclient.Client,
	systemCollector *system.Collector,
	logger *slog.Logger,
) *server.DomainAPIHandlers {
	return server.NewAPIHandler(
		deps.memberRepo,
		deps.memberCache,
		deps.cache,
		deps.profiles,
		deps.alarmCRUD,
		deps.holodexService,
		deps.youtubeService,
		scraperScheduler,
		deps.statsRepo,
		deps.activityLogger,
		deps.settings,
		settingsApplier,
		deps.acl,
		systemCollector,
		deps.templateAdminSvc,
		majorEventTriggerClient,
		majorEventTriggerClient,
		logger,
	).DomainHandlers()
}
