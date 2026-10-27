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
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v4/webhook"
	"github.com/park285/shared-go/v2/pkg/health"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	webhookHandler *webhook.Handler,
	readyProbe ...*sharedreadiness.Probe,
) (*gin.Engine, error) {
	out, err := sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		SkipLogPaths:           []string{"/l/*"},
		InternalReadyResponder: botReadyResponder(ctx, readiness.Pick(readyProbe...)),
		RegisterRoutes:         botRouteRegistrar(webhookHandler),
	})
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func botReadyResponder(ctx context.Context, probe *sharedreadiness.Probe) func(*gin.Context) {
	if probe != nil {
		return readiness.GinHandler(ctx, probe)
	}

	return func(c *gin.Context) {
		ginjson.Respond(c, http.StatusOK, gin.H{"health": health.Get()})
	}
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
