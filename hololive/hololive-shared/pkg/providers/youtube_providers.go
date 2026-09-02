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
	"log/slog"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func schedulerLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

// ProvideScraperService - 스크래퍼 서비스 생성.
func ProvideScraperService(
	cacheClient cache.Client,
	members domain.MemberDataProvider,
	proxyConfig scraper.ProxyConfig,
	sharedRL *ratelimiter.RateLimiter,
	logger *slog.Logger,
) *htmlscraper.Service {
	return ProvideScraperServiceWithOfficialSchedule(
		cacheClient,
		members,
		proxyConfig,
		sharedRL,
		logger,
		settings.LoadOfficialScheduleRuntimeConfig(),
	)
}

func ProvideScraperServiceWithOfficialSchedule(
	cacheClient cache.Client,
	members domain.MemberDataProvider,
	proxyConfig scraper.ProxyConfig,
	sharedRL *ratelimiter.RateLimiter,
	logger *slog.Logger,
	official settings.OfficialScheduleRuntimeConfig,
) *htmlscraper.Service {
	return htmlscraper.NewServiceWithOfficialSchedule(
		cacheClient,
		members,
		scraper.NewClient(scraper.WithProxy(proxyConfig), scraper.WithRateLimiter(sharedRL)),
		logger,
		official,
	)
}

func ProvideScraperServiceWithYouTubeClient(
	cacheClient cache.Client,
	members domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
) *htmlscraper.Service {
	return ProvideScraperServiceWithYouTubeClientAndSchedule(
		cacheClient,
		members,
		youtubeClient,
		logger,
		settings.LoadOfficialScheduleRuntimeConfig(),
	)
}

func ProvideScraperServiceWithYouTubeClientAndSchedule(
	cacheClient cache.Client,
	members domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
	official settings.OfficialScheduleRuntimeConfig,
) *htmlscraper.Service {
	return htmlscraper.NewServiceWithOfficialSchedule(cacheClient, members, youtubeClient, logger, official)
}

// ProvideScraperScheduler - YouTube HTML 스크래퍼 기반 폴러 스케줄러 생성
// 멤버 채널 목록을 조회하여 모든 폴러를 스케줄러에 등록한다.
func ProvideScraperScheduler(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts ...ScraperSchedulerOption,
) *pollscheduler.Scheduler {
	log := schedulerLogger(logger)
	resolvedOpts := resolveScraperSchedulerOptions(opts...)
	scheduler := newScraperScheduler(&resolvedOpts, log)
	channelPollerRegistrations := resolvedOpts.channelPollerRegistrations

	if len(channelPollerRegistrations) == 0 {
		log.Warn("Scraper scheduler initialized without poller registrations")

		return scheduler
	}

	allExplicit := allRegistrationsExplicit(channelPollerRegistrations)
	defaultChannelIDs, defaultTargetChannels := resolveDefaultScraperSchedulerChannels(membersData, log, &resolvedOpts, allExplicit)

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

	budgetRPM := 60.0 / settings.DefaultYouTubeOperationalConfig().RequestInterval.Seconds()
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

func newScraperScheduler(opts *scraperSchedulerOptions, logger *slog.Logger) *pollscheduler.Scheduler {
	if opts == nil {
		opts = &scraperSchedulerOptions{}
	}

	schedulerConfig := pollscheduler.DefaultSchedulerConfig()

	schedulerConfig.RequestInterval = 0
	schedulerConfig.Logger = logger

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
	schedulerConfig.BudgetLimiter = opts.budgetLimiter
	schedulerConfig.BudgetContext = opts.budgetContext

	if opts.budgetAcquireTimeout > 0 {
		schedulerConfig.BudgetAcquireTimeout = opts.budgetAcquireTimeout
	}

	return pollscheduler.NewScheduler(&schedulerConfig)
}

func resolveDefaultScraperSchedulerChannels(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts *scraperSchedulerOptions,
	allExplicit bool,
) (result0 []string, result1 int) {
	if opts == nil {
		opts = &scraperSchedulerOptions{}
	}

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
	scheduler *pollscheduler.Scheduler,
	logger *slog.Logger,
	registrations []ChannelPollerRegistration,
	defaultChannelIDs []string,
	distinctTargets map[string]struct{},
) (result0 int, result1, result2 float64) {
	totalJobs := 0

	var (
		totalRPM               float64
		totalRetryAmplifiedRPM float64
	)

	for i := range registrations {
		registration := &registrations[i]
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
			if err := scheduler.RegisterCheckedWithBudgetProfile(channelID, registration.Poller, registration.Priority, registration.Interval, registration.BudgetProfile); err != nil {
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
	for i := range registrations {
		registration := &registrations[i]
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

	for i := range registrations {
		registration := &registrations[i]
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

func explicitImplicitRegistrationMode(registration *ChannelPollerRegistration) int {
	if registration == nil {
		return 0
	}

	if registration.Poller == nil || registration.Interval <= 0 {
		return 0
	}

	if registration.HasExplicitChannelIDs {
		return 1
	}

	return 2
}
