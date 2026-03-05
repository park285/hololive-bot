package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

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
