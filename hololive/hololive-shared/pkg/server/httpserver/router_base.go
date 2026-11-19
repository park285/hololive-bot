package httpserver

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func newReleaseModeEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	return gin.New()
}

func applyBaseMiddleware(ctx context.Context, router *gin.Engine, logger *slog.Logger, skipLogPaths ...string) {
	if router == nil {
		return
	}

	router.Use(middleware.RecoveryMiddleware(ctx, logger))
	router.Use(middleware.LoggerMiddleware(ctx, logger, skipLogPaths...))
	router.Use(middleware.SecurityHeadersMiddleware())
	router.Use(middleware.RequestIDMiddleware())
	router.Use(middleware.MaxBodySizeMiddleware())
}
