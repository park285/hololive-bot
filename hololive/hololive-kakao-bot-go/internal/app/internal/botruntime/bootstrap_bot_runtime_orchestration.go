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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/quic-go/quic-go/http3"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

func buildBotRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger, infra *appbootstrap.BotInfrastructure) (*BotRuntime, error) {
	runtimeViews := buildBotRuntimeDependencyViews(infra)

	botBot, err := bot.NewBot(runtimeViews.botDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	webhookHandler, err := appbootstrap.BuildBotWebhookHandler(appConfig, botBot, runtimeViews.webhook, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot runtime: webhook handler: %w", err)
	}

	configSubscriber := appbootstrap.BuildBotConfigSubscriber(ctx, runtimeViews.configSubscriber, runtimeViews.configSubscriberRuntime, nil, logger)

	var botServer = nilHTTPServer()
	if appConfig.ServerTransportEnabled("h2c") {
		botServer, err = appbootstrap.BuildBotServer(ctx, appConfig, webhookHandler, nil, logger)
		if err != nil {
			return nil, err
		}
	}

	var h3Server *http3.Server
	if appConfig.ServerTransportEnabled("h3") {
		h3Server, err = appbootstrap.BuildBotHTTP3Server(ctx, appConfig, webhookHandler, nil, logger)
		if err != nil {
			return nil, err
		}
	}

	return &BotRuntime{
		Config:               appConfig,
		Logger:               logger,
		Bot:                  botBot,
		ConfigSubscriber:     configSubscriber,
		ServerAddr:           appConfig.Server.H2CAddr,
		HTTPServer:           botServer,
		H3Server:             h3Server,
		webhookHandlerCloser: webhookHandler,
	}, nil
}

func nilHTTPServer() *http.Server {
	return nil
}
