package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// ProvideHolodexAPIKeys - 설정에서 API 키 추출
func ProvideHolodexAPIKeys(cfg config.HolodexConfig) []string {
	return cfg.APIKeys
}

// ProvideScraperService - 스크래퍼 서비스 생성
func ProvideScraperService(
	cacheSvc *cache.Service,
	members *member.ServiceAdapter,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *holodex.ScraperService {
	return holodex.NewScraperService(cacheSvc, members, proxyConfig, sharedRL, logger)
}

// ProvideHolodexService - Holodex API 서비스 생성
func ProvideHolodexService(
	baseURL string,
	apiKeys []string,
	cacheSvc *cache.Service,
	scraperSvc *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	svc, err := holodex.NewHolodexService(baseURL, apiKeys, cacheSvc, scraperSvc, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}
	return svc, nil
}

// ProvideYouTubeStatsRepository - YouTube 통계 저장소 생성
func ProvideYouTubeStatsRepository(
	postgres *database.PostgresService,
	logger *slog.Logger,
) *youtube.StatsRepository {
	return youtube.NewYouTubeStatsRepository(postgres, logger)
}

// ProvideYouTubeStack - YouTube 서비스 스택 생성
func ProvideYouTubeStack(
	ctx context.Context,
	ytCfg config.YouTubeConfig,
	scraperCfg config.ScraperConfig,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	members *member.ServiceAdapter,
	statsRepo *youtube.StatsRepository,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
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

// ProvideScraperScheduler - YouTube HTML 스크래퍼 기반 폴러 스케줄러 생성
// 멤버 채널 목록을 조회하여 모든 폴러를 스케줄러에 등록한다.
func ProvideScraperScheduler(
	postgres *database.PostgresService,
	membersData domain.MemberDataProvider,
	intervals PollerIntervals,
	communityKeywords []string,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) *poller.Scheduler {
	// 스크래퍼 클라이언트 생성 (공유 RateLimiter 주입)
	scraperClient := scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheSvc),
	)
	db := postgres.GetGormDB()

	// 폴러 생성
	videosPoller := poller.NewVideosPoller(scraperClient, db, 10)
	shortsPoller := poller.NewShortsPoller(scraperClient, db, 10)
	communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords)
	statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
	livePoller := poller.NewLivePoller(scraperClient, db)

	// 스케줄러 생성 (RequestInterval=0: 외부 sharedRL에 rate limiting 위임)
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     2,
		RequestInterval: 0,
	})

	// 모든 멤버 채널에 대해 폴러 등록
	members := membersData.GetAllMembers()
	for _, m := range members {
		if m.IsGraduated {
			continue // 졸업 멤버 제외
		}

		channelID := m.ChannelID

		// 영상 폴러 (일반 우선순위)
		scheduler.Register(channelID, videosPoller, poller.PriorityNormal, intervals.Videos)

		// 쇼츠 폴러 (낮은 우선순위)
		scheduler.Register(channelID, shortsPoller, poller.PriorityLow, intervals.Shorts)

		// 커뮤니티 폴러 (낮은 우선순위)
		scheduler.Register(channelID, communityPoller, poller.PriorityLow, intervals.Community)

		// 채널 통계 폴러 (낮은 우선순위)
		scheduler.Register(channelID, statsPoller, poller.PriorityLow, intervals.Stats)

		// 라이브 폴러 (높은 우선순위)
		scheduler.Register(channelID, livePoller, poller.PriorityHigh, intervals.Live)
	}

	logger.Info("Scraper scheduler initialized",
		slog.Int("members", len(members)),
		slog.Int("total_jobs", len(members)*5)) // 5 pollers per member

	return scheduler
}
