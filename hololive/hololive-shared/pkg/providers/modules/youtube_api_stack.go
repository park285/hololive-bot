package modules

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/internal/service/youtube/apiservice"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type YouTubeAPIStackParams struct {
	YouTubeConfig   settings.YouTubeConfig
	CacheService    cache.Client
	MemberData      domain.MemberDataProvider
	SharedRateLimit *ratelimiter.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeAPIStack(ctx context.Context, params *YouTubeAPIStackParams) *providers.YouTubeStack {
	if params == nil {
		return &providers.YouTubeStack{}
	}

	service, err := apiservice.New(ctx, params.CacheService, params.MemberData, params.YouTubeConfig, params.SharedRateLimit, params.Logger)
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
