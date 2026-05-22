package youtube

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	apiservice "github.com/kapu/hololive-shared/pkg/service/youtube/internal/apiservice"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ChannelStats = apiservice.ChannelStats

type QuotaExceededError = apiservice.QuotaExceededError

func NewYouTubeService(
	ctx context.Context,
	apiKey string,
	cacheClient cache.Client,
	scraperProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (Service, error) {
	return apiservice.New(ctx, apiKey, cacheClient, scraperProxyConfig, sharedRL, logger)
}
