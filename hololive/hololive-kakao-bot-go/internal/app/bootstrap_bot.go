package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
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
	infra *coreInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) (*botAdminServerDependencies, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: config is nil")
	}
	if infra == nil || infra.deps == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: infra dependencies are nil")
	}

	deps := infra.deps

	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = cfg.Postgres.AutoPrepareSchema
	authService, err := authsvc.NewService(
		ctx,
		deps.Postgres.GetGormDB(),
		deps.Cache,
		logger,
		authCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("build bot admin server dependencies: create auth service: %w", err)
	}

	localSettingsApplier := sharedserver.NewLocalSettingsApplier(
		deps.Service,
		infra.holodexService,
		scraperScheduler,
		infra.alarmCRUD,
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

	var statsRepo youtube.StatsDashboardRepository
	if infra.ytStack != nil {
		statsRepo = infra.ytStack.StatsRepo
	}

	domainHandlers := server.NewAPIHandler(
		deps.MemberRepo,
		deps.MemberCache,
		deps.Cache,
		deps.Profiles,
		infra.alarmCRUD,
		infra.holodexService,
		deps.Service,
		scraperScheduler,
		statsRepo,
		deps.Activity,
		deps.Settings,
		settingsApplier,
		deps.ACL,
		systemCollector,
		infra.templateAdminSvc,
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

func buildYouTubeComponents(scraperCfg config.ScraperConfig, deps *bot.Dependencies, infra *coreInfrastructure, logger *slog.Logger) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}
	pollerRegistrations := buildBotChannelPollerRegistrations(
		deps.Postgres,
		scraperProxyConfig,
		infra.sharedRL,
		deps.Cache,
	)

	scraperScheduler := providers.ProvideScraperScheduler(
		deps.MembersData,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
	)

	outboxDispatcher := outbox.NewDispatcher(
		deps.Postgres.GetGormDB(),
		deps.Cache,
		deps.Client,
		infra.templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}

// buildBotRuntime 는 런타임 구성요소를 조립한다.
func buildBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, infra *coreInfrastructure) (*BotRuntime, error) {
	deps := infra.deps

	botBot, err := ProvideBot(deps)
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // worker goroutine은 task별 request context를 사용하므로 construction-time context 불필요
	webhookHandler := iris.NewWebhookHandler(cfg.Iris.WebhookToken, botBot, deps.Cache.GetClient(), logger, iris.WebhookHandlerOptions{
		WorkerCount:    cfg.Webhook.WorkerCount,
		QueueSize:      cfg.Webhook.QueueSize,
		EnqueueTimeout: cfg.Webhook.EnqueueTimeout,
		HandlerTimeout: cfg.Webhook.HandlerTimeout,
	})

	var (
		youtubeScheduler  youtube.Scheduler
		scraperScheduler  *poller.Scheduler
		photoSyncService  *holodex.PhotoSyncService
		outboxDispatcher  *outbox.Dispatcher
		alarmScheduler    runtimeAlarmScheduler
		ingestionLeaseRef *providers.IngestionLease
		desiredProxyState bool
	)
	if cfg.Bot.IngestionEnabled {
		logger.Warn("Bot ingestion runtime enabled",
			slog.String("event", "bot_ingestion_enabled"),
			slog.String("env", "BOT_INGESTION_ENABLED=true"),
			slog.String("lock_key", providers.IngestionLeaseKey),
			slog.String("note", "when stream-ingester is deployed, bot should usually run with BOT_INGESTION_ENABLED=false"),
		)

		ingestionLeaseRef, err = providers.AcquireIngestionLease(ctx, deps.Cache, "bot", logger)
		if err != nil {
			return nil, fmt.Errorf("acquire ingestion lease: %w", err)
		}

		scraperScheduler, outboxDispatcher = buildYouTubeComponents(cfg.Scraper, deps, infra, logger)
		youtubeScheduler = ProvideYouTubeScheduler(deps)
		photoSyncService = infra.photoSync

		desiredProxyState = deps.Settings.Get().ScraperProxyEnabled
		applyScraperProxyToggle(desiredProxyState, ProvideYouTubeService(infra.ytStack), infra.holodexService, scraperScheduler, logger)
	} else {
		logger.Info("Bot ingestion runtime disabled by config", slog.String("env", "BOT_INGESTION_ENABLED=false"))
	}

	// ConfigSubscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하여 적용
	configSubscriber := buildBotConfigSubscriber(deps, infra, scraperScheduler, logger)
	alarmScheduler = buildRuntimeAlarmScheduler(ctx, cfg, infra, logger)

	var adminServerDeps *botAdminServerDependencies
	if cfg.Bot.AdminEnabled {
		adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, infra, scraperScheduler, logger)
		if err != nil {
			return nil, fmt.Errorf("build bot runtime: admin server dependencies: %w", err)
		}
	}

	botServer, err := buildBotServer(ctx, cfg, webhookHandler, nil, infra.alarmCRUD, adminServerDeps, infra.deps.Cache, logger)
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

// buildBotConfigSubscriber: Bot용 ConfigSubscriber를 생성합니다.
// scraper_proxy / alarm_advance_minutes 두 가지 설정 변경을 수신하여 적용합니다.
func buildBotConfigSubscriber(
	deps *bot.Dependencies,
	infra *coreInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			applyScraperProxyToggle(payload.Enabled, ProvideYouTubeService(infra.ytStack), infra.holodexService, scraperScheduler, logger)
			// 설정 파일에도 반영
			current := deps.Settings.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := deps.Settings.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}
		},
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			targets := infra.alarmCRUD.UpdateAlarmAdvanceMinutes(payload.Minutes)
			logger.Info("Applied alarm advance minutes via pub/sub",
				slog.Int("minutes", payload.Minutes),
				slog.Any("targets", targets),
			)
			// 설정 파일에도 반영
			current := deps.Settings.Get()
			current.AlarmAdvanceMinutes = payload.Minutes
			if err := deps.Settings.Update(current); err != nil {
				logger.Warn("Failed to persist alarm_advance_minutes setting", slog.Any("error", err))
			}
		},
	})

	return configsub.New(deps.Cache.GetClient(), applyFn, logger)
}

// buildBotServer: Bot HTTP 서버를 구성합니다.
// - AdminEnabled=true: webhook + trigger + health + admin API
// - AdminEnabled=false: webhook + trigger + health
func buildBotServer(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	alarmCRUD domain.AlarmCRUD,
	adminDeps *botAdminServerDependencies,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*http.Server, error) {
	var (
		botRouter *gin.Engine
		err       error
	)

	if cfg.Bot.AdminEnabled {
		if adminDeps == nil || adminDeps.domainHandlers == nil || adminDeps.authHandler == nil {
			return nil, fmt.Errorf("build bot server: admin routes enabled but dependencies are incomplete")
		}
		botRouter, err = ProvideAPIRouter(
			ctx,
			cfg,
			logger,
			adminDeps.domainHandlers,
			adminDeps.authHandler,
			webhookHandler,
			triggerHandler,
			cacheSvc,
		)
		if err != nil {
			return nil, fmt.Errorf("build bot server: provide api router: %w", err)
		}
	} else {
		botRouter, err = ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
		if err != nil {
			return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
		}
	}

	if alarmCRUD != nil {
		if strings.TrimSpace(cfg.Server.APIKey) == "" {
			return nil, fmt.Errorf("build bot server: internal alarm API requires API_SECRET_KEY")
		}
		alarmAPI := alarmsvc.NewAPIHandler(alarmCRUD, logger)
		internalAlarmGroup := botRouter.Group("")
		internalAlarmGroup.Use(sharedserver.APIKeyAuthMiddleware(cfg.Server.APIKey))
		alarmAPI.RegisterInternalRoutes(internalAlarmGroup)
	}

	addr := ProvideAPIAddr(cfg)
	return ProvideAPIServer(addr, botRouter), nil
}

func applyScraperProxyToggle(
	enabled bool,
	youtubeService youtube.Service,
	holodexService *holodex.Service,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) {
	youtubeApplied := false
	holodexApplied := false
	schedulerApplied := 0

	if youtubeService != nil {
		youtubeApplied = youtubeService.SetScraperProxyEnabled(enabled)
	}
	if holodexService != nil {
		holodexApplied = holodexService.SetScraperProxyEnabled(enabled)
	}
	if scraperScheduler != nil {
		schedulerApplied = scraperScheduler.SetProxyEnabled(enabled)
	}

	logger.Info("Applied scraper proxy toggle",
		slog.Bool("enabled", enabled),
		slog.Bool("youtube_applied", youtubeApplied),
		slog.Bool("holodex_applied", holodexApplied),
		slog.Int("scheduler_pollers_applied", schedulerApplied),
	)
}
