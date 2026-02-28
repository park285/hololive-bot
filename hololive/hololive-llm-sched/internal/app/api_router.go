package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// ProvideAPIServer: HTTP 서버 인스턴스를 생성합니다.
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

// ProvideHealthOnlyRouter: health + metrics 엔드포인트만 제공하는 최소 라우터.
func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(sharedserver.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics",
	))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	return router, nil
}

// ProvideTriggerRouter: health + metrics + 내부 트리거 엔드포인트를 제공하는 라우터.
func ProvideTriggerRouter(
	ctx context.Context,
	logger *slog.Logger,
	triggerHandler *sharedserver.TriggerHandler,
) (*gin.Engine, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, err
	}

	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutes(router.Group(""))
	}

	return router, nil
}
