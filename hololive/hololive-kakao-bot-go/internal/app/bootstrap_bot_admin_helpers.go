package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
	authsvc "github.com/kapu/hololive-kakao-bot-go/internal/service/auth"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
	triggerclient "github.com/kapu/hololive-kakao-bot-go/internal/service/trigger"
)

type botAdminSettingsComponents struct {
	settingsApplier         sharedserver.SettingsApplier
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
	localSettingsApplier := sharedserver.NewLocalSettingsApplier(
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
			{Name: "llm-server", URL: cfg.Services.LLMServerHealthURL},
			{Name: "twentyq", URL: cfg.Services.GameBotTwentyQHealthURL},
			{Name: "turtlesoup", URL: cfg.Services.GameBotTurtleHealthURL},
		},
		cfg.Telemetry.Enabled,
	)
}

func buildBotAdminAPIHandlers(
	deps botAdminRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	settingsComponents botAdminSettingsComponents,
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
		settingsComponents.settingsApplier,
		deps.acl,
		systemCollector,
		deps.templateAdminSvc,
		settingsComponents.majorEventTriggerClient,
		settingsComponents.majorEventTriggerClient,
		logger,
	).DomainHandlers()
}
