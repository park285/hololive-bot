package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type YouTubeStackParams struct {
	YouTubeConfig   config.YouTubeConfig
	ScraperConfig   config.ScraperConfig
	CacheService    cache.Client
	SharedRateLimit *scraper.RateLimiter
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
