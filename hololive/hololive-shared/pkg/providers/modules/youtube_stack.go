package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type YouTubeStackParams struct {
	YouTubeConfig   settings.YouTubeConfig
	CacheService    cache.Client
	MemberData      domain.MemberDataProvider
	SharedRateLimit *ratelimiter.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeStack(ctx context.Context, params *YouTubeStackParams) *providers.YouTubeStack {
	if params == nil {
		return &providers.YouTubeStack{}
	}

	return BuildYouTubeAPIStack(ctx, &YouTubeAPIStackParams{
		YouTubeConfig:   params.YouTubeConfig,
		CacheService:    params.CacheService,
		MemberData:      params.MemberData,
		SharedRateLimit: params.SharedRateLimit,
		Logger:          params.Logger,
	})
}
