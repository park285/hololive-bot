package modules

import (
	"context"
	"log/slog"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type YouTubeStackParams struct {
	YouTubeConfig   config.YouTubeConfig
	ScraperConfig   config.ScraperConfig
	CacheService    cache.Client
	HolodexService  *holodex.Service
	Members         member.DataProvider
	StatsRepo       *ytstats.StatsRepository
	AlarmState      domain.AlarmDispatchState
	IrisClient      iris.Sender
	Formatter       youtube.MilestoneMessageFormatter
	SharedRateLimit *scraper.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeStack(ctx context.Context, params YouTubeStackParams) *providers.YouTubeStack {
	if !params.YouTubeConfig.EnableQuotaBuilding || params.YouTubeConfig.APIKey == "" {
		if params.Logger != nil {
			params.Logger.Info("YouTube quota building disabled; stats repository only")
		}
		return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
	}

	svc, err := youtube.NewYouTubeService(ctx, params.YouTubeConfig.APIKey, params.CacheService, scraper.ProxyConfig{
		Enabled: params.ScraperConfig.ProxyEnabled,
		URL:     params.ScraperConfig.ProxyURL,
	}, params.SharedRateLimit, params.Logger)
	if err != nil {
		if params.Logger != nil {
			params.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
		}
		return &providers.YouTubeStack{StatsRepo: params.StatsRepo}
	}

	scheduler := youtube.NewScheduler(
		svc,
		params.HolodexService,
		params.CacheService,
		params.StatsRepo,
		params.Members,
		params.AlarmState,
		params.IrisClient,
		params.Formatter,
		params.Logger,
	)

	if params.Logger != nil {
		params.Logger.Info("YouTube quota building enabled", slog.String("mode", "API Key"), slog.Int("daily_target", 9192))
	}

	return &providers.YouTubeStack{
		Service:   svc,
		Scheduler: scheduler,
		StatsRepo: params.StatsRepo,
	}
}
