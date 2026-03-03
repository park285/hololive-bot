package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

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

	scraperScheduler := providers.ProvideScraperScheduler(
		deps.Postgres,
		deps.MembersData,
		providers.DefaultPollerIntervals(),
		[]string{},
		scraperProxyConfig,
		infra.sharedRL,
		deps.Cache,
		logger,
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

	botServer, err := buildBotServer(ctx, cfg, webhookHandler, nil, infra.alarmCRUD, logger)
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
		ingestionLease:       ingestionLeaseRef,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           ProvideAPIAddr(cfg),
		HttpServer:           botServer,
		webhookHandlerCloser: webhookHandler,
	}, nil
}

// buildBotConfigSubscriber: Bot용 ConfigSubscriber를 생성합니다.
// scraper_proxy / alarm_advance_minutes 두 가지 설정 변경을 수신하여 적용합니다.
func buildBotConfigSubscriber(
	deps *bot.Dependencies,
	infra *coreInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
		case "scraper_proxy":
			var payload struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal scraper_proxy payload", slog.Any("error", err))
				return
			}
			applyScraperProxyToggle(payload.Enabled, ProvideYouTubeService(infra.ytStack), infra.holodexService, scraperScheduler, logger)
			// 설정 파일에도 반영
			current := deps.Settings.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := deps.Settings.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}

		case "alarm_advance_minutes":
			var payload struct {
				Minutes int `json:"minutes"`
			}
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal alarm_advance_minutes payload", slog.Any("error", err))
				return
			}
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

		default:
			logger.Warn("Unknown config update type", slog.String("type", update.Type))
		}
	}

	return configsub.New(deps.Cache.GetClient(), applyFn, logger)
}

// buildBotServer: Bot 전용 HTTP 서버를 구성합니다. (webhook + trigger + health만)
func buildBotServer(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) (*http.Server, error) {
	botRouter, err := ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
	if err != nil {
		return nil, err
	}

	if alarmCRUD != nil {
		alarmAPI := alarmsvc.NewAPIHandler(alarmCRUD, logger)
		alarmAPI.RegisterRoutes(botRouter.Group(""))
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
