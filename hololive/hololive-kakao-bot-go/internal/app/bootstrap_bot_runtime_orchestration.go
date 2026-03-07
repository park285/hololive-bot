package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
)

// buildBotRuntime 는 런타임 구성요소를 조립한다.
func buildBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, infra *coreInfrastructure) (*BotRuntime, error) {
	runtimeViews := buildBotRuntimeDependencyViews(infra)

	botBot, err := ProvideBot(runtimeViews.botDeps)
	if err != nil {
		return nil, err
	}

	webhookHandler := buildBotWebhookHandler(cfg, botBot, runtimeViews.webhook, logger)

	alarmScheduler, err := buildAlarmRuntimeScheduler(cfg, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot runtime: alarm runtime scheduler: %w", err)
	}

	// ConfigSubscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하여 적용
	configSubscriber := buildBotConfigSubscriber(runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)

	var adminServerDeps *botAdminServerDependencies
	if cfg.Bot.AdminEnabled {
		adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
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
		AlarmScheduler:       alarmScheduler,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           ProvideAPIAddr(cfg),
		HttpServer:           botServer,
		webhookHandlerCloser: webhookHandler,
	}, nil
}
