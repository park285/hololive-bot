package providers

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"
)

func ProvideYouTubeStack(
	ctx context.Context,
	ytCfg config.YouTubeConfig,
	scraperCfg config.ScraperConfig,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	members member.DataProvider,
	statsRepo *ytstats.StatsRepository,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Sender,
	formatter youtube.MilestoneMessageFormatter,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *YouTubeStack {
	if !ytCfg.EnableQuotaBuilding || ytCfg.APIKey == "" {
		logger.Info("YouTube quota building disabled; stats repository only")
		return &YouTubeStack{StatsRepo: statsRepo}
	}

	svc, err := youtube.NewYouTubeService(ctx, ytCfg.APIKey, cacheSvc, scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}, sharedRL, logger)
	if err != nil {
		logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
		return &YouTubeStack{StatsRepo: statsRepo}
	}

	scheduler := youtube.NewScheduler(svc, holodexSvc, cacheSvc, statsRepo, members, alarmSvc, irisClient, formatter, logger)
	logger.Info("YouTube quota building enabled",
		slog.String("mode", "API Key"),
		slog.Int("daily_target", 9192))

	return &YouTubeStack{
		Service:   svc,
		Scheduler: scheduler,
		StatsRepo: statsRepo,
	}
}
