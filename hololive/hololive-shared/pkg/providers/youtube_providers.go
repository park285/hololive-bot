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
	"strings"

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

func schedulerLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

// ProvideScraperService - 스크래퍼 서비스 생성
func ProvideScraperService(
	cacheClient cache.Client,
	members member.DataProvider,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *holodex.ScraperService {
	return holodex.NewScraperService(cacheClient, members, proxyConfig, sharedRL, logger)
}

func ProvideScraperServiceWithYouTubeProducer(
	cacheClient cache.Client,
	members member.DataProvider,
	youtubeProducer *scraper.Client,
	logger *slog.Logger,
) *holodex.ScraperService {
	return holodex.NewScraperServiceWithYouTubeProducer(cacheClient, members, youtubeProducer, logger)
}

// ProvideHolodexService - Holodex API 서비스 생성
func ProvideHolodexService(
	baseURL string,
	apiKey string,
	cacheClient cache.Client,
	scraperService *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	service, err := holodex.NewHolodexService(baseURL, apiKey, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}
	return service, nil
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
	log := schedulerLogger(logger)
	resolvedOpts := resolveScraperSchedulerOptions(opts...)
	scheduler := newScraperScheduler(resolvedOpts)
	channelPollerRegistrations := resolvedOpts.channelPollerRegistrations
	if len(channelPollerRegistrations) == 0 {
		log.Warn("Scraper scheduler initialized without poller registrations")
		return scheduler
	}

	allExplicit := allRegistrationsExplicit(channelPollerRegistrations)
	defaultChannelIDs, defaultTargetChannels := resolveDefaultScraperSchedulerChannels(membersData, log, resolvedOpts, allExplicit)
	if hasExplicitAndImplicitRegistrations(channelPollerRegistrations) {
		log.Warn("scraper scheduler has mixed explicit and default-backed registrations",
			slog.Int("poller_templates", len(channelPollerRegistrations)),
			slog.Int("default_target_channels", defaultTargetChannels))
	}

	distinctTargets := make(map[string]struct{}, len(defaultChannelIDs))
	totalJobs, totalRPM, totalRetryAmplifiedRPM := registerScraperSchedulerPollers(
		scheduler,
		log,
		channelPollerRegistrations,
		defaultChannelIDs,
		distinctTargets,
	)

	distinctTargetChannels := len(distinctTargets)
	log.Info("Scraper scheduler initialized",
		slog.Int("default_target_channels", defaultTargetChannels),
		slog.Int("distinct_target_channels", distinctTargetChannels),
		slog.Int("poller_templates", len(channelPollerRegistrations)),
		slog.Int("total_jobs", totalJobs),
		slog.Float64("expected_total_rpm", totalRPM),
		slog.Float64("expected_total_retry_amplified_rpm_max", totalRetryAmplifiedRPM))

	budgetRPM := 60.0 / constants.YouTubeProducerRateLimitConfig.RequestInterval.Seconds()
	if totalRPM > budgetRPM {
		log.Warn("scraper_poll_budget_exceeds_rate_limit",
			slog.Float64("expected_total_rpm", totalRPM),
			slog.Float64("budget_rpm", budgetRPM),
			slog.Int("distinct_target_channels", distinctTargetChannels),
			slog.Int("total_jobs", totalJobs),
		)
	}

	return scheduler
}

func newScraperScheduler(opts scraperSchedulerOptions) *poller.Scheduler {
	schedulerConfig := poller.DefaultSchedulerConfig()
	schedulerConfig.RequestInterval = 0
	if opts.workerCount > 0 {
		schedulerConfig.WorkerCount = opts.workerCount
	}
	if opts.pollTimeout > 0 {
		schedulerConfig.PollTimeout = opts.pollTimeout
	}
	if opts.errorBackoffMin > 0 {
		schedulerConfig.ErrorBackoffMin = opts.errorBackoffMin
	}
	if opts.errorBackoffMax > 0 {
		schedulerConfig.ErrorBackoffMax = opts.errorBackoffMax
	}
	schedulerConfig.JobClaimer = opts.jobClaimer
	return poller.NewScheduler(schedulerConfig)
}

func resolveDefaultScraperSchedulerChannels(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts scraperSchedulerOptions,
	allExplicit bool,
) ([]string, int) {
	defaultChannelIDs := uniqueChannelIDs(opts.channelIDs)
	defaultTargetChannels := len(defaultChannelIDs)
	if allExplicit || len(defaultChannelIDs) > 0 {
		return defaultChannelIDs, defaultTargetChannels
	}

	if membersData == nil {
		logger.Warn("Scraper scheduler initialized without members data")
		return defaultChannelIDs, defaultTargetChannels
	}

	members := membersData.GetAllMembers()
	defaultTargetChannels = len(members)
	defaultChannelIDs = make([]string, 0, len(members))
	for _, member := range members {
		if member == nil || member.IsGraduated {
			continue
		}
		defaultChannelIDs = append(defaultChannelIDs, member.ChannelID)
	}

	return uniqueChannelIDs(defaultChannelIDs), defaultTargetChannels
}

func registerScraperSchedulerPollers(
	scheduler *poller.Scheduler,
	logger *slog.Logger,
	registrations []ChannelPollerRegistration,
	defaultChannelIDs []string,
	distinctTargets map[string]struct{},
) (int, float64, float64) {
	totalJobs := 0
	var totalRPM float64
	var totalRetryAmplifiedRPM float64

	for _, registration := range registrations {
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

		registeredTargets := 0
		for _, channelID := range targetChannelIDs {
			if err := scheduler.RegisterChecked(channelID, registration.Poller, registration.Priority, registration.Interval); err != nil {
				logger.Warn("Skip invalid scraper poller registration",
					slog.String("channel_id", channelID),
					slog.String("poller", registration.Poller.Name()),
					slog.Any("error", err),
				)
				continue
			}

			distinctTargets[channelID] = struct{}{}
			registeredTargets++
		}

		pollerRPM := estimatedRegistrationRPM(registration, registeredTargets)
		pollerRetryAmplifiedRPM := estimatedRegistrationWorstCaseRPM(registration, registeredTargets)
		totalJobs += registeredTargets
		totalRPM += pollerRPM
		totalRetryAmplifiedRPM += pollerRetryAmplifiedRPM
		logger.Info("Scraper poller targets resolved",
			slog.String("poller", registration.Poller.Name()),
			slog.Int("target_channels", registeredTargets),
			slog.Duration("interval", registration.Interval),
			slog.Float64("request_units_per_run", estimatedRegistrationRequestUnitsPerRun(registration)),
			slog.Float64("worst_case_request_units_per_run", estimatedRegistrationWorstCaseRequestUnitsPerRun(registration)),
			slog.Float64("expected_rpm", pollerRPM),
			slog.Float64("expected_retry_amplified_rpm_max", pollerRetryAmplifiedRPM))
	}

	return totalJobs, totalRPM, totalRetryAmplifiedRPM
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
	const explicitAndImplicitRegistrations = 3

	observedRegistrations := 0
	for _, registration := range registrations {
		registrationMode := explicitImplicitRegistrationMode(registration)
		if registrationMode == 0 {
			continue
		}
		observedRegistrations |= registrationMode
		if observedRegistrations == explicitAndImplicitRegistrations {
			return true
		}
	}
	return false
}

func explicitImplicitRegistrationMode(registration ChannelPollerRegistration) int {
	if registration.Poller == nil || registration.Interval <= 0 {
		return 0
	}
	if registration.HasExplicitChannelIDs {
		return 1
	}
	return 2
}

func estimatedRegistrationRequestUnitsPerRun(registration ChannelPollerRegistration) float64 {
	requests := registration.RequestsPerRun
	if requests <= 0 {
		requests = 1
	}

	return float64(requests)
}

func estimatedRegistrationWorstCaseRequestUnitsPerRun(registration ChannelPollerRegistration) float64 {
	if registration.WorstCaseRequestUnitsPerRun > 0 {
		return registration.WorstCaseRequestUnitsPerRun
	}

	attempts := registration.WorstCaseAttempts
	if attempts <= 0 {
		attempts = 1
	}

	return estimatedRegistrationRequestUnitsPerRun(registration) * float64(attempts)
}

func estimatedRegistrationRPM(registration ChannelPollerRegistration, targetCount int) float64 {
	if registration.Interval <= 0 || targetCount <= 0 {
		return 0
	}

	return float64(targetCount) * (60.0 / registration.Interval.Seconds()) * estimatedRegistrationRequestUnitsPerRun(registration)
}

func estimatedRegistrationWorstCaseRPM(registration ChannelPollerRegistration, targetCount int) float64 {
	if registration.Interval <= 0 || targetCount <= 0 {
		return 0
	}

	return float64(targetCount) * (60.0 / registration.Interval.Seconds()) * estimatedRegistrationWorstCaseRequestUnitsPerRun(registration)
}

func estimatedRequestsPerMinute(registrations []ChannelPollerRegistration) float64 {
	var rpm float64
	for _, registration := range registrations {
		targetCount := 1
		if registration.HasExplicitChannelIDs {
			targetCount = len(uniqueChannelIDs(registration.ChannelIDs))
		}

		rpm += estimatedRegistrationRPM(registration, targetCount)
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
		channelID = strings.TrimSpace(channelID)
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
