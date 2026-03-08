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
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

// ProvideAPIAddr: 관리자 서버가 리슨할 주소를 반환합니다.
func ProvideAPIAddr(cfg *config.Config) string {
	return fmt.Sprintf(":%d", cfg.Server.Port)
}

// ProvideAPIServer: 관리자용 HTTP 서버 인스턴스를 생성합니다.
// H2C(HTTP/2 Cleartext)를 기본으로 사용하여 멀티플렉싱과 헤더 압축 이점을 제공한다.
func ProvideAPIServer(addr string, router *gin.Engine) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(router),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}

// ProvideAPIRouter: hololive-bot 도메인 API를 서빙하는 Gin 라우터를 설정합니다.
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
		return nil, fmt.Errorf("domain handlers must not be nil")
	}

	if authHandler == nil {
		return nil, fmt.Errorf("auth handler must not be nil")
	}

	if webhookHandler != nil {
		router.POST("/webhook/iris", webhookHandler.Handle)
	}

	// 내부 트리거 라우트 등록 (운영 API에서 스케줄러 수동 실행용)
	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	registerAPIRoutes(router, cfg.Server.APIKey, cacheSvc, logger, domainHandlers, authHandler)

	if cfg.Server.APIKey != "" {
		logger.Info("api_key_auth_enabled")
	} else {
		return nil, fmt.Errorf("API_SECRET_KEY required")
	}

	return router, nil
}

func newAPIRouter(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression)) // 응답 압축 (HTTP/2 호환)
	sharedserver.ApplyBaseMiddleware(router, ctx, logger, sharedserver.BaseMiddlewareOptions{
		SkipLogPaths: []string{
			"/health",
			"/ready",
			"/metrics", // Prometheus 메트릭 폴링 (15초 간격)
		},
	})
	isProduction := strings.EqualFold(strings.TrimSpace(cfg.Environment), "production")
	if isProduction && cfg.CORS.MissingInProduction {
		logger.Warn(
			"cors_allowed_origins_missing_in_production_monitor_mode",
			slog.Bool("cors_enforce", cfg.CORS.Enforce),
			slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
		)
	}
	router.Use(corsOriginGuard(cfg.CORS.AllowedOrigins))
	router.Use(cors.New(newAPICORSConfig(cfg)))
	router.Use(middleware.ClientHintsMiddleware()) // Client Hints 요청 (실제 기기 정보 수집)

	registerAPIHealthRoutes(router, cfg.Server.APIKey)

	// NoRoute 핸들러: 미등록 경로 접근 시 API Key 검증 후 401/404 반환
	// 크롤러/스캐너가 루트 경로 등에 접근할 때 404 대신 401 Unauthorized 반환
	router.NoRoute(middleware.NoRouteAuthHandler(cfg.Server.APIKey))

	return router, nil
}

func registerAPIHealthRoutes(router *gin.Engine, apiKey string) {
	sharedserver.RegisterHealthRoutes(router)
	// Prometheus 메트릭 (장기 히스토리 분석용)
	metrics := router.Group("")
	metrics.Use(middleware.APIKeyAuthMiddleware(apiKey))
	metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))
}
