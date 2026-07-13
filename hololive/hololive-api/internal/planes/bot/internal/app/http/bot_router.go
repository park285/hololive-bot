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
	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/iris-client-go/webhook"
	"github.com/park285/shared-go/pkg/ginjson"

	"github.com/kapu/hololive-api/internal/readiness"
)

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	readyProbe ...*readiness.Probe,
) (*gin.Engine, error) {
	return sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		InternalReadyResponder: botReadyResponder(ctx, readiness.Pick(readyProbe...)),
		RegisterRoutes:         botRouteRegistrar(appConfig.Server.APIKey, webhookHandler, triggerHandler, irisRoomLister, logger), //nolint:contextcheck // gin handler는 build ctx가 아니라 요청별 c.Request.Context()를 사용해야 한다.
	})
}

func botReadyResponder(ctx context.Context, probe *readiness.Probe) func(*gin.Context) {
	if probe != nil {
		return readiness.GinHandler(ctx, probe)
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"health": health.Get()})
	}
}

func botRouteRegistrar(
	apiKey string,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		registerWebhookRoute(router, webhookHandler)
		if err := registerIrisRoomRoute(router, apiKey, irisRoomLister, logger); err != nil {
			return err
		}
		return registerTriggerRoutes(router, apiKey, triggerHandler)
	}
}

func registerWebhookRoute(router *gin.Engine, webhookHandler *webhook.Handler) {
	if webhookHandler == nil {
		return
	}
	router.POST("/webhook/iris", gin.WrapH(webhookHandler))
}

func registerIrisRoomRoute(router *gin.Engine, apiKey string, roomLister IrisRoomLister, logger *slog.Logger) error {
	if roomLister == nil {
		return nil
	}
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("API_SECRET_KEY required")
	}
	router.GET(
		irisroomscontracts.ListPath,
		middleware.APIKeyAuthMiddleware(apiKey),
		handleIrisRooms(roomLister, logger),
	)
	return nil
}

func handleIrisRooms(roomLister IrisRoomLister, logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		resp, err := roomLister.GetRooms(c.Request.Context())
		if err != nil {
			logger.Error("Failed to list joined rooms from Iris", slog.Any("error", err))
			sharedserver.RespondError(c, http.StatusBadGateway, "Failed to list joined rooms", nil)
			return
		}
		if resp == nil {
			logger.Error("Failed to list joined rooms from Iris", slog.String("error", "nil response"))
			sharedserver.RespondError(c, http.StatusBadGateway, "Failed to list joined rooms", nil)
			return
		}
		ginjson.Respond(c, http.StatusOK, resp)
	}
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
