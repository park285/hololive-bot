package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func applyAPIRouterMiddleware(router *gin.Engine, ctx context.Context, cfg *config.Config, logger *slog.Logger) {
	router.Use(gin.Recovery())
	router.Use(sharedserver.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics", // Prometheus 메트릭 폴링 (15초 간격)
	))
	isProduction := strings.EqualFold(strings.TrimSpace(cfg.Telemetry.Environment), "production")
	if isProduction && cfg.CORS.MissingInProduction {
		logger.Warn(
			"cors_allowed_origins_missing_in_production_monitor_mode",
			slog.Bool("cors_enforce", cfg.CORS.Enforce),
			slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
		)
	}
	router.Use(corsOriginGuard(cfg.CORS.AllowedOrigins))
	router.Use(cors.New(newAPICORSConfig(cfg)))
	router.Use(sharedserver.SecurityHeadersMiddleware())
	router.Use(sharedserver.ClientHintsMiddleware()) // Client Hints 요청 (실제 기기 정보 수집)
}

func newAPICORSConfig(cfg *config.Config) cors.Config {
	corsConfig := cors.DefaultConfig()
	if len(cfg.CORS.AllowedOrigins) == 0 {
		corsConfig.AllowOriginFunc = func(string) bool { return false }
	} else {
		corsConfig.AllowOrigins = cfg.CORS.AllowedOrigins
	}
	corsConfig.AllowCredentials = true
	corsConfig.AllowMethods = constants.CORSConfig.AllowMethods
	corsConfig.AllowHeaders = constants.CORSConfig.AllowHeaders
	return corsConfig
}

func corsOriginGuard(allowedOrigins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			allowAll = true
			continue
		}
		allowed[trimmed] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" || allowAll {
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
