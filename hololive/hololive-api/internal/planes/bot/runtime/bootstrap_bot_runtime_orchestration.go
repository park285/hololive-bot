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

package botruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/quic-go/quic-go/http3"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func newBotReadyProbe(infra *appbootstrap.BotInfrastructure) *sharedreadiness.Probe {
	return sharedreadiness.NewProbe("bot",
		sharedreadiness.PostgresCheck(infra.Postgres),
		sharedreadiness.ValkeyCheck(infra.Cache),
	)
}

func buildBotOptionalServers(ctx context.Context, appConfig *settings.Config) (metricsServer, pprofServer *http.Server) {
	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		metricsServer = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey)
	}
	if pprofAddr := strings.TrimSpace(appConfig.Server.PprofAddr); pprofAddr != "" {
		pprofServer = sharedserver.NewPprofServer(ctx, pprofAddr, appConfig.Server.APIKey)
	}
	return metricsServer, pprofServer
}

func buildBotRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger, infra *appbootstrap.BotInfrastructure) (*BotRuntime, error) {
	if appConfig == nil {
		return nil, fmt.Errorf("build bot runtime: app config is nil")
	}
	if infra == nil {
		return nil, fmt.Errorf("build bot runtime: infra is nil")
	}

	runtimeViews := buildBotRuntimeDependencyViews(infra)

	botBot, err := orchestration.NewBot(runtimeViews.botDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	durable, err := buildDurableRuntime(infra, botBot, appConfig.Webhook.WorkerCount, appConfig.Webhook.HandlerTimeout, logger)
	if err != nil {
		return nil, err
	}
	configureDurableReplyWriter(botBot, durable, logger)

	webhookHandler, err := appbootstrap.BuildDurableBotWebhookHandler(appConfig, durableAdmitter{
		inbox:  durable.inbox,
		wake:   func() { notifyDurable(durable.inboxWake) },
		logger: logger,
	}, runtimeViews.webhook, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot runtime: webhook handler: %w", err)
	}

	configSubscriber := appbootstrap.BuildBotConfigSubscriber(ctx, runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)

	readyProbe := newBotReadyProbe(infra)

	var h3Server *http3.Server
	var h3CertReloadStart func(context.Context)
	if appConfig.ServerTransportEnabled("h3") {
		h3Server, h3CertReloadStart, err = appbootstrap.BuildBotHTTP3Server(ctx, appConfig, webhookHandler, nil, infra.IrisRoomLister, logger, readyProbe)
		if err != nil {
			return nil, err
		}
	}

	metricsServer, pprofServer := buildBotOptionalServers(ctx, appConfig)

	return &BotRuntime{
		Config:               appConfig,
		Logger:               logger,
		Bot:                  botBot,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           appConfig.Server.H3Addr,
		H3Server:             h3Server,
		MetricsServer:        metricsServer,
		PprofServer:          pprofServer,
		h3CertReloadStart:    h3CertReloadStart,
		webhookHandlerCloser: webhookHandler,
		durable:              durable,
	}, nil
}

func configureDurableReplyWriter(bot *orchestration.Bot, durable *durableRuntime, logger *slog.Logger) {
	bot.SetReplyOutboxWriter(durableReplyWriter{
		outbox: durable.outbox,
		logger: logger,
		wake:   func() { notifyDurable(durable.outboxWake) },
	})
}

func buildDurableRuntime(infra *appbootstrap.BotInfrastructure, bot *orchestration.Bot, workers int, handlerTimeout time.Duration, logger *slog.Logger) (*durableRuntime, error) {
	if infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, fmt.Errorf("build bot runtime: durable postgres pool is nil")
	}
	return newDurableRuntime(bot, infra.Deps.Client, infra.Postgres.GetPool(), workers, handlerTimeout, logger), nil
}
