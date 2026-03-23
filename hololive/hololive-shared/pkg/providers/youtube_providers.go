// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"
)

// ProvideHolodexAPIKey - 설정에서 API 키 추출
func ProvideHolodexAPIKey(cfg config.HolodexConfig) string {
	return cfg.APIKey
}

// ProvideScraperService - 스크래퍼 서비스 생성
func ProvideScraperService(
	cacheSvc cache.Client,
	members member.DataProvider,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *holodex.ScraperService {
	return holodex.NewScraperService(cacheSvc, members, proxyConfig, sharedRL, logger)
}

// ProvideHolodexService - Holodex API 서비스 생성
func ProvideHolodexService(
	baseURL string,
	apiKey string,
	cacheSvc cache.Client,
	scraperSvc *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	svc, err := holodex.NewHolodexService(baseURL, apiKey, cacheSvc, scraperSvc, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}
	return svc, nil
}

// ProvideYouTubeStatsRepository - YouTube 통계 저장소 생성
func ProvideYouTubeStatsRepository(
	postgres database.Client,
	logger *slog.Logger,
) *ytstats.StatsRepository {
	return ytstats.NewYouTubeStatsRepository(postgres, logger)
}

// ProvideYouTubeStack - YouTube 서비스 스택 생성
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

// ProvideScraperScheduler - YouTube HTML 스크래퍼 기반 폴러 스케줄러 생성
// 멤버 채널 목록을 조회하여 모든 폴러를 스케줄러에 등록한다.
func ProvideScraperScheduler(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts ...ScraperSchedulerOption,
) *poller.Scheduler {
	// 스케줄러 생성 (RequestInterval=0: 외부 sharedRL에 rate limiting 위임)
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     2,
		RequestInterval: 0,
	})

	resolvedOpts := resolveScraperSchedulerOptions(opts...)
	channelPollerRegistrations := resolvedOpts.channelPollerRegistrations
	if len(channelPollerRegistrations) == 0 {
		logger.Warn("Scraper scheduler initialized without poller registrations")
		return scheduler
	}

	// 모든 멤버 채널에 대해 폴러 등록
	members := membersData.GetAllMembers()
	for _, m := range members {
		if m.IsGraduated {
			continue // 졸업 멤버 제외
		}

		channelID := m.ChannelID

		for _, registration := range channelPollerRegistrations {
			if registration.Poller == nil || registration.Interval <= 0 {
				continue
			}
			scheduler.Register(channelID, registration.Poller, registration.Priority, registration.Interval)
		}
	}

	logger.Info("Scraper scheduler initialized",
		slog.Int("members", len(members)),
		slog.Int("poller_templates", len(channelPollerRegistrations)),
		slog.Int("total_jobs", len(members)*len(channelPollerRegistrations)))

	return scheduler
}
