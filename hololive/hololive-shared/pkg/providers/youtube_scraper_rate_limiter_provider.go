package providers

import (
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// ProvideYouTubeScraperRateLimiter: YouTube HTML 스크래퍼용 공유 레이트리미터를 생성합니다.
// 분산 제한이 활성화된 경우 Valkey 기반 SlidingWindowLimiter를 함께 구성합니다.
func ProvideYouTubeScraperRateLimiter(cacheSvc cache.Client, logger *slog.Logger) (*scraper.RateLimiter, error) {
	limiter := scraper.NewRateLimiter(constants.YouTubeScraperRateLimitConfig.RequestInterval)

	if !constants.YouTubeScraperDistributedRateLimitConfig.Enabled {
		return limiter, nil
	}

	distributedLimiter, err := ratelimit.NewSlidingWindowLimiter(
		cacheSvc,
		constants.YouTubeScraperDistributedRateLimitConfig.KeyPrefix,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize scraper distributed rate limiter: %w", err)
	}
	if err := limiter.ConfigureDistributed(
		distributedLimiter,
		constants.YouTubeScraperDistributedRateLimitConfig.Limit,
		constants.YouTubeScraperDistributedRateLimitConfig.Window,
	); err != nil {
		return nil, fmt.Errorf("configure scraper distributed rate limiter: %w", err)
	}

	return limiter, nil
}
