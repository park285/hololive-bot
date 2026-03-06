package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

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
		ingestionLease:       ingestionLeaseRef,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           ProvideAPIAddr(cfg),
		HttpServer:           botServer,
		webhookHandlerCloser: webhookHandler,
	}, nil
}
