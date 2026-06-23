package scraping

import "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter"

func IsDistributedRateLimiterUnavailable(err error) bool {
	return ratelimiter.IsDistributedLimiterUnavailable(err)
}
