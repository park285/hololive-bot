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
	"errors"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// ProvideHealthOnlyRouter: health + metrics 엔드포인트만 제공하는 최소 라우터.
func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, apiKey string) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}

	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	sharedserver.ApplyBaseMiddleware(router, ctx, logger, sharedserver.BaseMiddlewareOptions{
		SkipLogPaths: []string{
			"/health",
			"/ready",
			"/metrics",
		},
	})

	registerAPIHealthRoutes(router, apiKey)

	return router, nil
}

// ProvideTriggerRouter: health + metrics + 내부 트리거 엔드포인트를 제공하는 라우터.
func ProvideTriggerRouter(ctx context.Context, logger *slog.Logger, triggerHandler *sharedserver.TriggerHandler, apiKey string) (*gin.Engine, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger, apiKey)
	if err != nil {
		return nil, err
	}

	if triggerHandler != nil {
		if strings.TrimSpace(apiKey) == "" {
			return nil, errors.New("API_SECRET_KEY required")
		}

		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
	}

	return router, nil
}

// ProvideBotRouter: Bot 전용 라우터를 구성합니다. (webhook + internal trigger + health만)
// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}

	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	sharedserver.ApplyBaseMiddleware(router, ctx, logger, sharedserver.BaseMiddlewareOptions{
		SkipLogPaths: []string{
			"/health",
			"/ready",
			"/metrics",
		},
	})

	registerAPIHealthRoutes(router, cfg.Server.APIKey)

	// Iris webhook 수신 (h2c POST)
	if webhookHandler != nil {
		router.POST("/webhook/iris", webhookHandler.Handle)
	}

	// 내부 트리거 라우트 (운영 API에서 내부 호출)
	if triggerHandler != nil {
		if strings.TrimSpace(cfg.Server.APIKey) == "" {
			return nil, errors.New("API_SECRET_KEY required")
		}

		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	return router, nil
}
