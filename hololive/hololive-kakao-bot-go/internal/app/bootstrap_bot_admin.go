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

type memberNewsWeeklyRunNowTrigger interface {
	SendMemberNewsWeekly(ctx context.Context) error
}

type botAdminServerDependencies struct {
	domainHandlers *server.DomainAPIHandlers
	authHandler    *server.AuthHandler
}

type botSettingsApplier struct {
	base             sharedserver.SettingsApplier
	memberNewsRunNow memberNewsWeeklyRunNowTrigger
	logger           *slog.Logger
}

func newBotSettingsApplier(
	base sharedserver.SettingsApplier,
	memberNewsRunNow memberNewsWeeklyRunNowTrigger,
	logger *slog.Logger,
) sharedserver.SettingsApplier {
	if logger == nil {
		logger = slog.Default()
	}

	return &botSettingsApplier{
		base:             base,
		memberNewsRunNow: memberNewsRunNow,
		logger:           logger,
	}
}

func (a *botSettingsApplier) ApplyScraperProxy(ctx context.Context, enabled bool) sharedserver.ScraperProxyApplyResult {
	if a.base == nil {
		applied := false
		return sharedserver.ScraperProxyApplyResult{
			Requested: enabled,
			Applied:   &applied,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ApplyScraperProxy(ctx, enabled)
}

func (a *botSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) sharedserver.AlarmAdvanceMinutesApplyResult {
	if a.base == nil {
		return sharedserver.AlarmAdvanceMinutesApplyResult{
			AlarmRequestedAdvanceMinutes: minutes,
			AlarmApplied:                 false,
			AlarmReason:                  "settings applier not configured",
		}
	}
	return a.base.ApplyAlarmAdvanceMinutes(ctx, minutes)
}

func (a *botSettingsApplier) ApplyMemberNewsWeeklyRunNow(ctx context.Context) sharedserver.MemberNewsWeeklyRunNowResult {
	if a.memberNewsRunNow == nil {
		return sharedserver.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger is not configured",
		}
	}

	if err := a.memberNewsRunNow.SendMemberNewsWeekly(ctx); err != nil {
		a.logger.Warn("Failed to trigger member news weekly run-now", slog.Any("error", err))
		return sharedserver.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger failed",
			Error:   err.Error(),
		}
	}

	return sharedserver.MemberNewsWeeklyRunNowResult{
		Applied: true,
		Source:  "member_news_trigger",
	}
}

func (a *botSettingsApplier) ScraperProxyRuntimeState(requested bool) sharedserver.ScraperProxyRuntimeStateResult {
	if a.base == nil {
		known := false
		return sharedserver.ScraperProxyRuntimeStateResult{
			Requested: requested,
			Known:     &known,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ScraperProxyRuntimeState(requested)
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
		return nil, fmt.Errorf("build bot admin server dependencies: create auth service: %w", err)
	}

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

	systemCollector := system.NewCollector(
		[]system.ServiceEndpoint{
			{Name: "llm-server", URL: cfg.Services.LLMServerHealthURL},
			{Name: "twentyq", URL: cfg.Services.GameBotTwentyQHealthURL},
			{Name: "turtlesoup", URL: cfg.Services.GameBotTurtleHealthURL},
		},
		cfg.Telemetry.Enabled,
	)

	domainHandlers := server.NewAPIHandler(
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

	return &botAdminServerDependencies{
		domainHandlers: domainHandlers,
		authHandler:    server.NewAuthHandler(authService, logger),
	}, nil
}
