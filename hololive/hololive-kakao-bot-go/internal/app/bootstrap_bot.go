package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/server"
	authsvc "github.com/kapu/hololive-kakao-bot-go/internal/service/auth"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
	triggerclient "github.com/kapu/hololive-kakao-bot-go/internal/service/trigger"
)

type runtimeAlarmSchedulerBuilder func(context.Context, *config.Config, *coreInfrastructure, *slog.Logger) runtimeAlarmScheduler

func defaultRuntimeAlarmSchedulerBuilder(context.Context, *config.Config, *coreInfrastructure, *slog.Logger) runtimeAlarmScheduler {
	return nil
}

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

// InitializeBotDependencies - 봇 의존성을 초기화합니다.
func InitializeBotDependencies(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*bot.Dependencies, func(), error) {
	infra, err := initCoreInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return infra.deps, cleanup, nil
}

// InitializeBotRuntime - cmd/bot 런타임 (Bot + MQ + Admin API 구성요소)
func InitializeBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*BotRuntime, func(), error) {
	infra, err := initCoreInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	runtime, err := buildBotRuntime(ctx, cfg, logger, infra)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return runtime, cleanup, nil
}

// ProvideBot: 봇 인스턴스를 생성하여 제공함
func ProvideBot(deps *bot.Dependencies) (*bot.Bot, error) {
	created, err := bot.NewBot(deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}
	return created, nil
}

// ProvideYouTubeService: YouTube 서비스 인스턴스를 제공합니다.
func ProvideYouTubeService(ytStack *providers.YouTubeStack) youtube.Service {
	if ytStack == nil {
		return nil
	}
	return ytStack.Service
}

// ProvideYouTubeScheduler: YouTube 스케줄러 인스턴스를 제공합니다.
func ProvideYouTubeScheduler(deps *bot.Dependencies) youtube.Scheduler {
	if deps == nil {
		return nil
	}
	return deps.Scheduler
}

// ProvideTriggerHandler: 내부 트리거 핸들러를 생성하여 제공합니다.
func ProvideTriggerHandler(
	majorEventScheduler server.MajorEventScheduler,
	majorEventMonthlyScheduler server.MajorEventMonthlyScheduler,
	memberNewsWeeklyScheduler sharedserver.MemberNewsWeeklyScheduler,
	logger *slog.Logger,
) *sharedserver.TriggerHandler {
	return sharedserver.NewTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsWeeklyScheduler, logger)
}

func buildYouTubeComponents(
	scraperCfg config.ScraperConfig,
	deps botIngestionRuntimeDependencies,
	runtimeDeps botYouTubeRuntimeDependencies,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}
	pollerRegistrations := buildBotChannelPollerRegistrations(
		deps.postgres,
		scraperProxyConfig,
		runtimeDeps.sharedRateLimiter,
		deps.cache,
	)

	scraperScheduler := providers.ProvideScraperScheduler(
		deps.members,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
	)

	outboxDispatcher := outbox.NewDispatcher(
		deps.postgres.GetGormDB(),
		deps.cache,
		deps.irisClient,
		runtimeDeps.templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}

func buildBotWebhookHandler(
	cfg *config.Config,
	messageHandler iris.MessageHandler,
	deps botWebhookRuntimeDependencies,
	logger *slog.Logger,
) *iris.WebhookHandler {
	//nolint:contextcheck // worker goroutine은 task별 request context를 사용하므로 construction-time context 불필요
	return iris.NewWebhookHandler(cfg.Iris.WebhookToken, messageHandler, deps.cache.GetClient(), logger, iris.WebhookHandlerOptions{
		WorkerCount:    cfg.Webhook.WorkerCount,
		QueueSize:      cfg.Webhook.QueueSize,
		EnqueueTimeout: cfg.Webhook.EnqueueTimeout,
		HandlerTimeout: cfg.Webhook.HandlerTimeout,
	})
}

// buildBotRuntime 는 런타임 구성요소를 조립한다.
func buildBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, infra *coreInfrastructure) (*BotRuntime, error) {
	runtimeViews := buildBotRuntimeDependencyViews(infra)

	botBot, err := ProvideBot(runtimeViews.botDeps)
	if err != nil {
		return nil, err
	}

	webhookHandler := buildBotWebhookHandler(cfg, botBot, runtimeViews.webhook, logger)

	var (
		youtubeScheduler  youtube.Scheduler
		scraperScheduler  *poller.Scheduler
		photoSyncService  *holodex.PhotoSyncService
		outboxDispatcher  *outbox.Dispatcher
		alarmScheduler    runtimeAlarmScheduler
		ingestionLeaseRef *providers.IngestionLease
	)
	ingestionComponents, err := buildBotRuntimeIngestion(ctx, cfg, runtimeViews, logger)
	if err != nil {
		return nil, err
	}
	youtubeScheduler = ingestionComponents.scheduler
	scraperScheduler = ingestionComponents.scraperScheduler
	photoSyncService = ingestionComponents.photoSyncService
	outboxDispatcher = ingestionComponents.outboxDispatcher
	ingestionLeaseRef = ingestionComponents.ingestionLease

	// ConfigSubscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하여 적용
	configSubscriber := buildBotConfigSubscriber(runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, scraperScheduler, logger)
	alarmScheduler = buildRuntimeAlarmScheduler(ctx, cfg, infra, logger)

	var adminServerDeps *botAdminServerDependencies
	if cfg.Bot.AdminEnabled {
		adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, scraperScheduler, logger)
		if err != nil {
			return nil, fmt.Errorf("build bot runtime: admin server dependencies: %w", err)
		}
	}

	botServer, err := buildBotServer(ctx, cfg, webhookHandler, nil, runtimeViews.serverRuntime.alarmCRUD, adminServerDeps, logger)
	if err != nil {
		return nil, err
	}

	return &BotRuntime{
		Config:               cfg,
		Logger:               logger,
		Bot:                  botBot,
		IngestionEnabled:     cfg.Bot.IngestionEnabled,
		Scheduler:            youtubeScheduler,
		ScraperScheduler:     scraperScheduler,
		PhotoSync:            photoSyncService,
		OutboxDispatcher:     outboxDispatcher,
		AlarmScheduler:       alarmScheduler,
		ingestionLease:       ingestionLeaseRef,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           ProvideAPIAddr(cfg),
		HttpServer:           botServer,
		webhookHandlerCloser: webhookHandler,
	}, nil
}

func buildRuntimeAlarmScheduler(
	ctx context.Context,
	cfg *config.Config,
	infra *coreInfrastructure,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if infra == nil || infra.runtimeAlarmSchedulerBuilder == nil {
		return nil
	}
	return infra.runtimeAlarmSchedulerBuilder(ctx, cfg, infra, logger)
}
