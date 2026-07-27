package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type YouTubeStackParams struct {
	YouTubeConfig   settings.YouTubeConfig
	ScraperConfig   settings.ScraperConfig
	CacheService    cache.Client
	SharedRateLimit *ratelimiter.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeStack(ctx context.Context, params *YouTubeStackParams) *providers.YouTubeStack {
	if params == nil {
		return &providers.YouTubeStack{}
	}
	return BuildYouTubeAPIStack(ctx, &YouTubeAPIStackParams{
		YouTubeConfig:   params.YouTubeConfig,
		ScraperConfig:   params.ScraperConfig,
		CacheService:    params.CacheService,
		SharedRateLimit: params.SharedRateLimit,
		Logger:          params.Logger,
	})
}
