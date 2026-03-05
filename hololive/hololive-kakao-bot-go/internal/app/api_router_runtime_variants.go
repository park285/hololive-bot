package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// ProvideHealthOnlyRouter: health + metrics 엔드포인트만 제공하는 최소 라우터.
func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(middleware.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics",
	))

	registerAPIHealthRoutes(router)

	return router, nil
}

// ProvideTriggerRouter: health + metrics + 내부 트리거 엔드포인트를 제공하는 라우터.
func ProvideTriggerRouter(ctx context.Context, logger *slog.Logger, triggerHandler *sharedserver.TriggerHandler, apiKey string) (*gin.Engine, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, err
	}

	if triggerHandler != nil {
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
	router.Use(middleware.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics",
	))

	registerAPIHealthRoutes(router)

	// Iris webhook 수신 (h2c POST)
	if webhookHandler != nil {
		router.POST("/webhook/iris", webhookHandler.Handle)
	}

	// 내부 트리거 라우트 (운영 API에서 내부 호출)
	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	return router, nil
}
