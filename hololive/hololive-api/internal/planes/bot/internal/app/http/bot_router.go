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
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/readiness"
)

// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	readyProbe ...*readiness.Probe,
) (*gin.Engine, error) {
	return sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:         appConfig.Server.APIKey,
		ReadyResponder: botReadyResponder(readiness.Pick(readyProbe...)), //nolint:contextcheck // gin readiness 핸들러는 c.Request.Context()로 요청 컨텍스트를 전달(표준 HTTP 경계)
		RegisterRoutes: botRouteRegistrar(appConfig.Server.APIKey, webhookHandler, triggerHandler),
	})
}

func botReadyResponder(probe *readiness.Probe) func(*gin.Context) {
	if probe != nil {
		return readiness.GinHandler(probe)
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"health": health.Get()})
	}
}

func botRouteRegistrar(
	apiKey string,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		registerWebhookRoute(router, webhookHandler)
		return registerTriggerRoutes(router, apiKey, triggerHandler)
	}
}

func registerWebhookRoute(router *gin.Engine, webhookHandler *iris.WebhookHandler) {
	if webhookHandler == nil {
		return
	}
	router.POST("/webhook/iris", gin.WrapH(webhookHandler))
}

func registerTriggerRoutes(
	router *gin.Engine,
	apiKey string,
	triggerHandler *sharedserver.TriggerHandler,
) error {
	if triggerHandler == nil {
		return nil
	}
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("API_SECRET_KEY required")
	}
	triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
	return nil
}
