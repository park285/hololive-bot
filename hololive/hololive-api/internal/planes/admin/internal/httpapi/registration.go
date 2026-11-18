package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	ratelimitvalkey "github.com/kapu/hololive-shared/pkg/service/ratelimit/valkey"
)

const (
	apiRateLimitPerWindow = 60
	apiRateLimitWindow    = time.Minute
)

type apiRateLimitHandler struct {
	limiter *ratelimitvalkey.SlidingWindowLimiter
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
	cacheClient cache.LowLevelCache,
	logger *slog.Logger,
	domainHandlers *api.DomainHandlers,
) error {
	if domainHandlers == nil {
		return errors.New("domain handlers must not be nil")
	}

	domains := domainHandlers

	holoAPI := router.Group("/api/holo")

	holoAPI.Use(middleware.APIKeyAuthMiddleware(apiKey))

	rateLimit, err := apiRateLimitMiddleware(cacheClient, logger)
	if err != nil {
		return fmt.Errorf("register /api/holo rate limit: %w", err)
	}

	holoAPI.Use(rateLimit)

	registerMemberRoutes(holoAPI, domains.Member)
	registerAlarmRoutes(holoAPI, domains.Alarm)
	registerRoomRoutes(holoAPI, domains.Room)
	registerStatsRoutes(holoAPI, domains.Stats)
	registerStreamRoutes(holoAPI, domains.Stream)
	registerSettingsRoutes(holoAPI, domains.Settings)
	registerSchedulePosterRoutes(holoAPI, domains.SchedulePoster)

	return nil
}

func apiRateLimitMiddleware(cacheClient cache.LowLevelCache, logger *slog.Logger) (gin.HandlerFunc, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if cacheClient == nil {
		return nil, errors.New("api rate limit requires a cache client")
	}

	limiter, err := ratelimitvalkey.NewSlidingWindowLimiter(cacheClient, "api:holo:ip", logger)
	if err != nil {
		return nil, fmt.Errorf("init api rate limiter: %w", err)
	}

	handler := apiRateLimitHandler{
		limiter: limiter,
		limit:   apiRateLimitPerWindow,
		window:  apiRateLimitWindow,
		logger:  logger,
	}

	return handler.Handle, nil
}

func (h apiRateLimitHandler) Handle(c *gin.Context) {
	ip := c.ClientIP()
	if ip == "" {
		abortWithRateLimitError(c)

		return
	}

	decision, err := h.limiter.Allow(c.Request.Context(), ip, h.limit, h.window)
	if err != nil {
		h.logger.Warn("api_rate_limit_check_failed", slog.String("ip", ip), slog.Any("error", err))
		apiRateLimitCheckFailuresTotal.Inc()
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "rate limit unavailable", nil)
		c.Abort()

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
