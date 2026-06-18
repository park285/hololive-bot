package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/kapu/hololive-shared/pkg/service/youtube/youtubefactory"
)

type YouTubeAPIStackParams struct {
	YouTubeConfig   config.YouTubeConfig
	ScraperConfig   config.ScraperConfig
	CacheService    cache.Client
	StatsRepository *ytstats.StatsRepository
	SharedRateLimit *scraper.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeAPIStack(ctx context.Context, params *YouTubeAPIStackParams) *providers.YouTubeStack {
	if params == nil {
		return &providers.YouTubeStack{}
	}
	if !params.YouTubeConfig.EnableQuotaBuilding || params.YouTubeConfig.APIKey == "" {
		if params.Logger != nil {
			params.Logger.Info("YouTube quota building disabled; stats repository only")
		}
		return &providers.YouTubeStack{StatsRepository: params.StatsRepository}
	}

	service, err := youtubefactory.NewYouTubeService(ctx, params.YouTubeConfig.APIKey, params.CacheService, scraper.ProxyConfig{
		Enabled: params.ScraperConfig.ProxyEnabled,
		URL:     params.ScraperConfig.ProxyURL,
	}, params.SharedRateLimit, params.Logger)
	if err != nil {
		if params.Logger != nil {
			params.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
		}
		return &providers.YouTubeStack{StatsRepository: params.StatsRepository}
	}

	return &providers.YouTubeStack{
		Service:         service,
		StatsRepository: params.StatsRepository,
	}
}
