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
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

// Admin Dashboard와 Tauri 앱에서 사용됩니다.
func ProvideAPIRouter(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	cacheSvc cache.Client,
) (*gin.Engine, error) {
	router, err := newAPIRouter(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	if domainHandlers == nil {
		return nil, errors.New("domain handlers must not be nil")
	}

	if authHandler == nil {
		return nil, errors.New("auth handler must not be nil")
	}

	if webhookHandler != nil {
		router.POST("/webhook/iris", gin.WrapH(webhookHandler))
	}

	// 내부 트리거 라우트 등록 (운영 API에서 스케줄러 수동 실행용)
	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	registerAPIRoutes(router, cfg.Server.APIKey, cacheSvc, logger, domainHandlers, authHandler)

	if cfg.Server.APIKey != "" {
		logger.Info("api_key_auth_enabled")
	} else {
		return nil, errors.New("API_SECRET_KEY required")
	}

	return router, nil
}

func newAPIRouter(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*gin.Engine, error) {
	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey:       cfg.Server.APIKey,
		EnableGzip:   true,
		SkipLogPaths: []string{"/metrics"},
		PreRouteUse: []gin.HandlerFunc{
			corsOriginGuard(cfg.CORS.AllowedOrigins),
			cors.New(newAPICORSConfig(cfg)),
			middleware.ClientHintsMiddleware(),
		},
	})
	if err != nil {
		return nil, err
	}

	isProduction := strings.EqualFold(strings.TrimSpace(cfg.Environment), "production")
	if isProduction && cfg.CORS.MissingInProduction {
		logger.Warn(
			"cors_allowed_origins_missing_in_production_monitor_mode",
			slog.Bool("cors_enforce", cfg.CORS.Enforce),
			slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
		)
	}

	// NoRoute 핸들러: 미등록 경로 접근 시 API Key 검증 후 401/404 반환
	// 크롤러/스캐너가 루트 경로 등에 접근할 때 404 대신 401 Unauthorized 반환
	router.NoRoute(middleware.NoRouteAuthHandler(cfg.Server.APIKey))

	return router, nil
}
