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
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
	"github.com/quic-go/quic-go/http3"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

type BotRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Bot *bot.Bot

	ConfigSubscriber *configsub.Subscriber

	ServerAddr string
	HttpServer *http.Server
	H3Server   *http3.Server

	webhookHandlerCloser interface{ Close() error }
	lifecycle.Managed
}

func BuildRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*BotRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	runtime, cleanup, err := InitializeBotRuntime(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", err)
	}

	runtime.Managed = lifecycle.NewManaged(cleanup)

	return runtime, nil
}
