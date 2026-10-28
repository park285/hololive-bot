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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/quic-go/quic-go/http3"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func newBotReadyProbe(postgres sharedreadiness.Pinger, cacheClient sharedreadiness.ConnectionChecker) *sharedreadiness.Probe {
	return sharedreadiness.NewProbe("bot",
		sharedreadiness.PostgresCheck(postgres),
		sharedreadiness.ValkeyCheck(cacheClient),
	)
}

func buildBotOptionalServers(ctx context.Context, appConfig *apiconfig.BotPlaneConfig) (metricsServer, pprofServer *http.Server) {
	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		metricsServer = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey)
	}

	if pprofAddr := strings.TrimSpace(appConfig.Server.PprofAddr); pprofAddr != "" {
		pprofServer = sharedserver.NewPprofServer(ctx, pprofAddr, appConfig.Server.APIKey)
	}

	return metricsServer, pprofServer
}

func buildBotRuntime(ctx context.Context, appConfig *apiconfig.BotPlaneConfig, logger *slog.Logger, infra *appbootstrap.BotInfrastructure) (_ *BotRuntime, retErr error) {
	if appConfig == nil {
		return nil, errors.New("app config is nil")
	}

	if infra == nil {
		return nil, errors.New("infra is nil")
	}

	if infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, errors.New("durable postgres pool is nil")
	}

	// bot은 모든 응답을 이 outbox에 기록하므로 bot보다 먼저 만든다.
	outbox, err := newDurableReplyOutbox(infra.Postgres.GetPool(), appConfig.APIWorkerProfile)
	if err != nil {
		return nil, fmt.Errorf("build reply outbox: %w", err)
	}

	infra.Deps.ReplyOutbox = outbox.writer(logger)

	botBot, err := orchestration.NewBot(infra.Deps)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	durable, err := newDurableRuntime(botBot, infra.Deps.Client, infra.Postgres.GetPool(), outbox, appConfig.APIWorkerProfile, logger)
	if err != nil {
		return nil, fmt.Errorf("build durable runtime: %w", err)
	}

	webhookHandler, err := appbootstrap.BuildDurableBotWebhookHandler(appConfig, durableAdmitter{
		inbox:  durable.inbox,
		wake:   func() { notifyDurable(durable.inboxWake) },
		accept: botBot.AcceptsMessage,
		logger: logger,
		totals: durable.inboxTotals,
	}, appbootstrap.BotWebhookRuntimeDependencies{Cache: infra.Cache}, logger)
	if err != nil {
		return nil, fmt.Errorf("build webhook handler: %w", err)
	}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, webhookHandler.CloseContext(ctx))
		}
	}()

	readyProbe := newBotReadyProbe(infra.Postgres, infra.Cache)

	var (
		h3Server          *http3.Server
		h3CertReloadStart func(context.Context)
	)

	if appConfig.Server.TransportEnabled("h3") {
		h3Server, h3CertReloadStart, err = appbootstrap.BuildBotHTTP3Server(ctx, appConfig, webhookHandler, logger, readyProbe)
		if err != nil {
			return nil, fmt.Errorf("build bot HTTP3 server: %w", err)
		}
	}

	metricsServer, pprofServer := buildBotOptionalServers(ctx, appConfig)

	return assembleBotRuntime(appConfig, logger, botBot, h3Server, h3CertReloadStart, metricsServer, pprofServer, webhookHandler, durable), nil
}

func assembleBotRuntime(
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	botBot *orchestration.Bot,
	h3Server *http3.Server,
	h3CertReloadStart func(context.Context),
	metricsServer, pprofServer *http.Server,
	webhookHandler interface{ CloseContext(context.Context) error },
	durable *durableRuntime,
) *BotRuntime {
	return &BotRuntime{
		Config:               appConfig,
		Logger:               logger,
		Bot:                  botBot,
		ServerAddr:           appConfig.Server.H3Addr,
		H3Server:             h3Server,
		ShortLinkServer:      appbootstrap.BuildShortLinkServer(appConfig.Server.ShortLinkAddr),
		MetricsServer:        metricsServer,
		PprofServer:          pprofServer,
		h3CertReloadStart:    h3CertReloadStart,
		webhookHandlerCloser: webhookHandler,
		durable:              durable,
	}
}
