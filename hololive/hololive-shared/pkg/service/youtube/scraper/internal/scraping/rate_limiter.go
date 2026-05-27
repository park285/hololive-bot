package scraping

import (
	"net/url"
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter"
)

type RateLimiter = ratelimiter.RateLimiter

var NewRateLimiter = ratelimiter.New

func distributedBucketFromURL(pageURL string) string {
	base := ytDefaults.ProducerDistributedRateLimit.BucketBase
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return base + ":unknown"
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		path = "root"
	}
	path = strings.ReplaceAll(path, "/", ":")
	return base + ":" + path
}
