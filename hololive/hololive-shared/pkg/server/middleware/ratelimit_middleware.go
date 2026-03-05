package middleware

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

const (
	// apiRateLimitKeyPrefix: API 레이트 리밋 전용 Valkey 키 접두사
	apiRateLimitKeyPrefix = "ratelimit:sliding:api"
)

// RateLimitMiddleware: IP 기반 슬라이딩 윈도우 레이트 리밋 미들웨어를 반환합니다.
// cacheSvc가 nil이면 레이트 리밋을 건너뜁니다 (graceful degradation).
// 한도 초과 시 429 Too Many Requests와 Retry-After 헤더를 반환합니다.
func RateLimitMiddleware(cacheSvc cache.Client, limit int, window time.Duration, logger *slog.Logger) gin.HandlerFunc {
	if cacheSvc == nil {
		// 캐시 서비스 미제공 시 레이트 리밋 비활성화
		if logger != nil {
			logger.Warn("rate_limit_middleware_disabled", slog.String("reason", "cache service is nil"))
		}
		return func(c *gin.Context) { c.Next() }
	}

	limiter, err := ratelimit.NewSlidingWindowLimiter(cacheSvc, apiRateLimitKeyPrefix, logger)
	if err != nil {
		// 리미터 생성 실패 시 graceful degradation (요청 차단하지 않음)
		if logger != nil {
			logger.Error("rate_limit_middleware_init_failed",
				slog.String("error", err.Error()),
				slog.String("reason", "falling back to passthrough"),
			)
		}
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		bucket := "ip:" + ip

		decision, err := limiter.Allow(c.Request.Context(), bucket, limit, window)
		if err != nil {
			// 레이트 리밋 판정 실패 시 요청 허용 (graceful degradation)
			if logger != nil {
				logger.Error("rate_limit_check_failed",
					slog.String("ip", ip),
					slog.String("error", err.Error()),
				)
			}
			c.Next()
			return
		}

		if !decision.Allowed {
			retryAfterSec := int(math.Ceil(decision.RetryAfter.Seconds()))
			if retryAfterSec < 1 {
				retryAfterSec = 1
			}
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfterSec))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "too_many_requests",
				"message":     "rate limit exceeded",
				"retry_after": retryAfterSec,
			})
			return
		}

		c.Next()
	}
}
