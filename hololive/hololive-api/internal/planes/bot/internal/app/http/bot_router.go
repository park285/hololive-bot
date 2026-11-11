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

package apphttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v5/webhook"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	webhookHandler *webhook.Handler,
	readyProbe *sharedreadiness.Probe,
) (*gin.Engine, error) {
	if readyProbe == nil {
		return nil, errors.New("ready probe must not be nil")
	}

	out, err := sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		SkipLogPaths:           []string{"/l/*"},
		InternalReadyResponder: readiness.GinHandler(ctx, readyProbe),
		RegisterRoutes:         botRouteRegistrar(webhookHandler),
	})
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func botRouteRegistrar(webhookHandler *webhook.Handler) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		registerShortLinkRoutes(router)
		registerWebhookRoute(router, webhookHandler)

		return nil
	}
}

func registerWebhookRoute(router *gin.Engine, webhookHandler *webhook.Handler) {
	if webhookHandler == nil {
		return
	}

	router.POST("/webhook/iris", gin.WrapH(webhookHandler))
}
