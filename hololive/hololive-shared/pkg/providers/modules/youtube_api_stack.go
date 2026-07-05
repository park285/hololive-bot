package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/youtubefactory"
)

type YouTubeAPIStackParams struct {
	YouTubeConfig   config.YouTubeConfig
	ScraperConfig   config.ScraperConfig
	CacheService    cache.Client
	SharedRateLimit *scraper.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeAPIStack(ctx context.Context, params *YouTubeAPIStackParams) *providers.YouTubeStack {
	if params == nil {
		return &providers.YouTubeStack{}
	}

	service, err := youtubefactory.NewYouTubeService(ctx, params.CacheService, scraper.ProxyConfig{
		Enabled: params.ScraperConfig.ProxyEnabled,
		URL:     params.ScraperConfig.ProxyURL,
	}, params.SharedRateLimit, params.Logger)
	if err != nil {
		if params.Logger != nil {
			params.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
		}
		return &providers.YouTubeStack{}
	}

	return &providers.YouTubeStack{
		Service: service,
	}
}
