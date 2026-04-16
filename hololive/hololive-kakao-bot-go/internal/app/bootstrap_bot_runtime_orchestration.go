// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

// buildBotRuntime 는 런타임 구성요소를 조립한다.
func buildBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger, infra *appbootstrap.CoreInfrastructure) (*BotRuntime, error) {
	runtimeViews := buildBotRuntimeDependencyViews(infra)

	botBot, err := bot.NewBot(runtimeViews.botDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	webhookHandler, err := appbootstrap.BuildBotWebhookHandler(cfg, botBot, runtimeViews.webhook, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot runtime: webhook handler: %w", err)
	}

	alarmScheduler, err := buildRuntimeAlarmScheduler(runtimeRoleBot, cfg, infra, logger, os.Getenv(notificationSchedulerRoleEnv))
	if err != nil {
		return nil, fmt.Errorf("build bot runtime: alarm runtime scheduler: %w", err)
	}

	// ConfigSubscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하여 적용
	configSubscriber := appbootstrap.BuildBotConfigSubscriber(ctx, runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)

	var adminServerDeps *botAdminServerDependencies

	if cfg.Bot.AdminEnabled {
		adminServerDeps, err = buildBotAdminServerDependencies(ctx, cfg, runtimeViews.adminRuntime, nil, logger)
		if err != nil {
			return nil, fmt.Errorf("build bot runtime: admin server dependencies: %w", err)
		}
	}

	var internalAlarmCRUD domain.AlarmCRUD
	if cfg.Bot.AdminEnabled {
		internalAlarmCRUD = runtimeViews.serverRuntime.alarmCRUD
	}

	botServer, err := appbootstrap.BuildBotServer(ctx, cfg, webhookHandler, nil, internalAlarmCRUD, adminServerDeps, logger)
	if err != nil {
		return nil, err
	}

	return &BotRuntime{
		Config:               cfg,
		Logger:               logger,
		Bot:                  botBot,
		AlarmScheduler:       alarmScheduler,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           fmt.Sprintf(":%d", cfg.Server.Port),
		HttpServer:           botServer,
		webhookHandlerCloser: webhookHandler,
	}, nil
}
