package app

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
)

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
