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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

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

// ProvideScraperScheduler - YouTube HTML 스크래퍼 기반 폴러 스케줄러 생성
// 멤버 채널 목록을 조회하여 모든 폴러를 스케줄러에 등록한다.
func ProvideScraperScheduler(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts ...ScraperSchedulerOption,
) *poller.Scheduler {
	resolvedOpts := resolveScraperSchedulerOptions(opts...)

	schedulerCfg := poller.DefaultSchedulerConfig()
	schedulerCfg.RequestInterval = 0
	if resolvedOpts.workerCount > 0 {
		schedulerCfg.WorkerCount = resolvedOpts.workerCount
	}
	scheduler := poller.NewScheduler(schedulerCfg)

	channelPollerRegistrations := resolvedOpts.channelPollerRegistrations
	if len(channelPollerRegistrations) == 0 {
		logger.Warn("Scraper scheduler initialized without poller registrations")
		return scheduler
	}

	defaultChannelIDs := uniqueChannelIDs(resolvedOpts.channelIDs)
	defaultTargetChannels := len(defaultChannelIDs)
	allExplicit := allRegistrationsExplicit(channelPollerRegistrations)
	if hasExplicitAndImplicitRegistrations(channelPollerRegistrations) {
		logger.Warn("scraper scheduler has mixed explicit and default-backed registrations",
			slog.Int("poller_templates", len(channelPollerRegistrations)),
			slog.Int("default_target_channels", defaultTargetChannels))
	}
	if !allExplicit && len(defaultChannelIDs) == 0 {
		if membersData == nil {
			logger.Warn("Scraper scheduler initialized without members data")
		} else {
			members := membersData.GetAllMembers()
			defaultTargetChannels = len(members)
			defaultChannelIDs = make([]string, 0, len(members))
			for _, m := range members {
				if m == nil || m.IsGraduated {
					continue
				}
				defaultChannelIDs = append(defaultChannelIDs, m.ChannelID)
			}
			defaultChannelIDs = uniqueChannelIDs(defaultChannelIDs)
		}
	}

	distinctTargets := make(map[string]struct{}, len(defaultChannelIDs))
	totalJobs := 0
	var totalRPM float64

	for _, registration := range channelPollerRegistrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}

		targetChannelIDs := defaultChannelIDs
		if registration.HasExplicitChannelIDs {
			targetChannelIDs = uniqueChannelIDs(registration.ChannelIDs)
		}
		if len(targetChannelIDs) == 0 {
			continue
		}

		for _, channelID := range targetChannelIDs {
			scheduler.Register(channelID, registration.Poller, registration.Priority, registration.Interval)
			distinctTargets[channelID] = struct{}{}
		}

		pollerRPM := float64(len(targetChannelIDs)) * (60.0 / registration.Interval.Seconds())
		totalJobs += len(targetChannelIDs)
		totalRPM += pollerRPM
		logger.Info("Scraper poller targets resolved",
			slog.String("poller", registration.Poller.Name()),
			slog.Int("target_channels", len(targetChannelIDs)),
			slog.Duration("interval", registration.Interval),
			slog.Float64("expected_rpm", pollerRPM))
	}

	distinctTargetChannels := len(distinctTargets)
	logger.Info("Scraper scheduler initialized",
		slog.Int("default_target_channels", defaultTargetChannels),
		slog.Int("distinct_target_channels", distinctTargetChannels),
		slog.Int("poller_templates", len(channelPollerRegistrations)),
		slog.Int("total_jobs", totalJobs),
		slog.Float64("expected_total_rpm", totalRPM))

	budgetRPM := 60.0 / constants.YouTubeScraperRateLimitConfig.RequestInterval.Seconds()
	if totalRPM > budgetRPM {
		logger.Warn("scraper_poll_budget_exceeds_rate_limit",
			slog.Float64("expected_total_rpm", totalRPM),
			slog.Float64("budget_rpm", budgetRPM),
			slog.Int("distinct_target_channels", distinctTargetChannels),
			slog.Int("total_jobs", totalJobs),
		)
	}

	return scheduler
}

func allRegistrationsExplicit(registrations []ChannelPollerRegistration) bool {
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if !registration.HasExplicitChannelIDs {
			return false
		}
	}
	return true
}

func hasExplicitAndImplicitRegistrations(registrations []ChannelPollerRegistration) bool {
	hasExplicit := false
	hasImplicit := false
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if registration.HasExplicitChannelIDs {
			hasExplicit = true
		} else {
			hasImplicit = true
		}
		if hasExplicit && hasImplicit {
			return true
		}
	}
	return false
}

func estimatedRequestsPerMinute(registrations []ChannelPollerRegistration) float64 {
	var rpm float64
	for _, registration := range registrations {
		if registration.Interval <= 0 {
			continue
		}
		rpm += 60.0 / registration.Interval.Seconds()
	}
	return rpm
}

func uniqueChannelIDs(channelIDs []string) []string {
	if len(channelIDs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(channelIDs))
	unique := make([]string, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID == "" {
			continue
		}
		if _, exists := seen[channelID]; exists {
			continue
		}
		seen[channelID] = struct{}{}
		unique = append(unique, channelID)
	}

	return unique
}
