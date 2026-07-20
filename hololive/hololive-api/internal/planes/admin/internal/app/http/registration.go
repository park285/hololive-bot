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
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

type apiRateLimitHandler struct {
	limiter *ratelimit.SlidingWindowLimiter
	limit   int
	window  time.Duration
	logger  *slog.Logger
}

func abortWithRateLimitError(c *gin.Context) {
	sharedserver.RespondError(c, http.StatusTooManyRequests, "too many requests", nil)
	c.Abort()
}

func registerAPIRoutes(
	router *gin.Engine,
	apiKey string,
	cacheClient cache.Client,
	logger *slog.Logger,
	domainHandlers *server.DomainHandlers,
	authHandler *server.AuthHandler,
	adminAllowedIPs []*net.IPNet,
) error {
	if domainHandlers == nil {
		return errors.New("domain handlers must not be nil")
	}

	domains := domainHandlers

	// OAuth 콜백 프록시 (인증 불필요 - Google에서 직접 호출)
	// 모바일 앱에서 localhost 리디렉션이 불가능하므로 서버가 프록시 역할
	router.GET("/oauth/callback", domains.OAuth.OAuthCallbackHandler)

	// Session 기반 인증 API. Login/password reset은 외부 진입점이고,
	// register는 관리자 계정 생성면이므로 API key로 보호한다.
	// admin-api는 Tailscale 직결이므로 RemoteAddr 기준 ip_allowlist로 추가 차단한다.
	authAPI := router.Group("/api/auth")
	authAPI.Use(middleware.AdminIPAllowMiddleware(adminAllowedIPs, logger))
	authAPI.POST("/login", authHandler.Login)
	authAPI.POST("/logout", authHandler.Logout)
	authAPI.POST("/refresh", authHandler.Refresh)
	authAPI.GET("/me", authHandler.Me)
	authAPI.POST("/password/reset-request", authHandler.ResetRequest)
	authAPI.POST("/password/reset", authHandler.ResetPassword)

	authAdminAPI := router.Group("/api/auth")
	authAdminAPI.Use(middleware.AdminIPAllowMiddleware(adminAllowedIPs, logger))
	authAdminAPI.Use(middleware.APIKeyAuthMiddleware(apiKey))
	authAdminAPI.POST("/register", authHandler.Register)

	// hololive-bot 도메인 API (Admin Dashboard, Tauri 앱에서 사용)
	holoAPI := router.Group("/api/holo")

	holoAPI.Use(middleware.APIKeyAuthMiddleware(apiKey))

	if constants.APIRateLimitConfig.Enabled {
		holoAPI.Use(apiRateLimitMiddleware(cacheClient, logger))
	}

	registerMemberRoutes(holoAPI, domains.Member)
	registerAlarmRoutes(holoAPI, domains.Alarm)
	registerRoomRoutes(holoAPI, domains.Room)
	registerStatsRoutes(holoAPI, domains.Stats, domains.Stream)
	registerStreamRoutes(holoAPI, domains.Stream)
	registerSettingsRoutes(holoAPI, domains.Settings)
	registerTemplateRoutes(holoAPI, domains.Template)
	registerProfileRoutes(holoAPI, domains.Profile)
	registerMajorEventRoutes(holoAPI, domains.MajorEvent)

	return nil
}

func apiRateLimitMiddleware(cacheClient cache.Client, logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	if cacheClient == nil {
		logger.Warn("api_rate_limit_disabled_no_cache")
		return func(c *gin.Context) { c.Next() }
	}

	limiter, err := ratelimit.NewSlidingWindowLimiter(cacheClient, "api:holo:ip", logger)
	if err != nil {
		logger.Error("api_rate_limit_init_failed", slog.String("error", err.Error()))
		return func(c *gin.Context) { c.Next() }
	}

	limit := constants.APIRateLimitConfig.Limit
	window := constants.APIRateLimitConfig.Window

	handler := apiRateLimitHandler{
		limiter: limiter,
		limit:   limit,
		window:  window,
		logger:  logger,
	}

	return handler.Handle
}

func (h apiRateLimitHandler) Handle(c *gin.Context) {
	ip := c.ClientIP()
	if ip == "" {
		abortWithRateLimitError(c)
		return
	}

	decision, err := h.limiter.Allow(c.Request.Context(), ip, h.limit, h.window)
	if err != nil {
		h.logger.Warn("api_rate_limit_check_failed", slog.String("ip", ip), slog.String("error", err.Error()))
		c.Next()

		return
	}

	c.Header("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
	c.Header("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))

	if !decision.Allowed {
		if decision.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
		}

		abortWithRateLimitError(c)

		return
	}

	c.Next()
}
