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
	StatsRepository       *ytstats.StatsRepository
	AlarmState      domain.AlarmDispatchState
	IrisClient      iris.Sender
	Formatter       youtube.MilestoneMessageFormatter
	SharedRateLimit *scraper.RateLimiter
	Logger          *slog.Logger
}

func BuildYouTubeStack(ctx context.Context, params YouTubeStackParams) *providers.YouTubeStack {
	stack := BuildYouTubeAPIStack(ctx, YouTubeAPIStackParams{
		YouTubeConfig:   params.YouTubeConfig,
		ScraperConfig:   params.ScraperConfig,
		CacheService:    params.CacheService,
		StatsRepository:       params.StatsRepository,
		SharedRateLimit: params.SharedRateLimit,
		Logger:          params.Logger,
	})
	if stack.Service == nil {
		return stack
	}

	scheduler := youtube.NewScheduler(
		stack.Service,
		params.HolodexService,
		params.CacheService,
		params.StatsRepository,
		params.Members,
		params.AlarmState,
		params.IrisClient,
		params.Formatter,
		params.Logger,
	)

	if params.Logger != nil {
		params.Logger.Info("YouTube quota building enabled", slog.String("mode", "API Key"), slog.Int("daily_target", 9192))
	}

	stack.Scheduler = scheduler
	return stack
}
