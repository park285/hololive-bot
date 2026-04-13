package runtime

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type youtubeScraperBudgetSummary struct {
	PollerRPM                 float64
	PollerRetryAmplifiedRPM   float64
	ResolverRPM               float64
	ResolverRetryAmplifiedRPM float64
	CombinedRPM               float64
	CombinedRetryAmplifiedRPM float64
	BudgetRPM                 float64
}

func summarizeYouTubeScraperBudget(registrations []providers.ChannelPollerRegistration) youtubeScraperBudgetSummary {
	pollerRPM := 0.0
	pollerRetryAmplifiedRPM := 0.0
	resolverRPM := 0.0
	resolverRetryAmplifiedRPM := 0.0
	for _, registration := range registrations {
		rpm := estimateRegistrationRPM(registration)
		retryAmplifiedRPM := estimateRegistrationWorstCaseRPM(registration)
		if isPublishedAtResolverRegistration(registration) {
			resolverRPM += rpm
			resolverRetryAmplifiedRPM += retryAmplifiedRPM
			continue
		}
		pollerRPM += rpm
		pollerRetryAmplifiedRPM += retryAmplifiedRPM
	}
	combinedRPM := pollerRPM + resolverRPM
	combinedRetryAmplifiedRPM := pollerRetryAmplifiedRPM + resolverRetryAmplifiedRPM
	budgetRPM := 60.0 / constants.YouTubeScraperRateLimitConfig.RequestInterval.Seconds()

	return youtubeScraperBudgetSummary{
		PollerRPM:                 pollerRPM,
		PollerRetryAmplifiedRPM:   pollerRetryAmplifiedRPM,
		ResolverRPM:               resolverRPM,
		ResolverRetryAmplifiedRPM: resolverRetryAmplifiedRPM,
		CombinedRPM:               combinedRPM,
		CombinedRetryAmplifiedRPM: combinedRetryAmplifiedRPM,
		BudgetRPM:                 budgetRPM,
	}
}

func validateYouTubeScraperPollerBudget(summary youtubeScraperBudgetSummary) error {
	if summary.CombinedRPM <= summary.BudgetRPM {
		return nil
	}
	return fmt.Errorf(
		"stream-ingester combined active scraper RPM %.3f exceeds YouTube scraper budget %.3f; increase poll intervals or reduce target channels",
		summary.CombinedRPM,
		summary.BudgetRPM,
	)
}

func logYouTubeScraperBudgetSummary(summary youtubeScraperBudgetSummary, logger *slog.Logger) {
	if logger == nil {
		return
	}

	logger.Info("youtube_scraper_combined_budget_summary",
		slog.Float64("expected_poller_rpm", summary.PollerRPM),
		slog.Float64("expected_poller_retry_amplified_rpm_max", summary.PollerRetryAmplifiedRPM),
		slog.Float64("expected_resolver_rpm", summary.ResolverRPM),
		slog.Float64("expected_resolver_retry_amplified_rpm_max", summary.ResolverRetryAmplifiedRPM),
		slog.Float64("expected_combined_rpm", summary.CombinedRPM),
		slog.Float64("expected_combined_retry_amplified_rpm_max", summary.CombinedRetryAmplifiedRPM),
		slog.Float64("budget_rpm", summary.BudgetRPM),
	)
	if summary.CombinedRPM > summary.BudgetRPM {
		logger.Warn("youtube_scraper_combined_budget_exceeds_rate_limit",
			slog.Float64("expected_poller_rpm", summary.PollerRPM),
			slog.Float64("expected_resolver_rpm", summary.ResolverRPM),
			slog.Float64("expected_combined_rpm", summary.CombinedRPM),
			slog.Float64("budget_rpm", summary.BudgetRPM),
		)
	}
	if summary.CombinedRetryAmplifiedRPM > summary.BudgetRPM {
		logger.Warn("youtube_scraper_fault_envelope_exceeds_rate_limit",
			slog.Float64("expected_poller_rpm", summary.PollerRPM),
			slog.Float64("expected_poller_retry_amplified_rpm_max", summary.PollerRetryAmplifiedRPM),
			slog.Float64("expected_resolver_rpm", summary.ResolverRPM),
			slog.Float64("expected_resolver_retry_amplified_rpm_max", summary.ResolverRetryAmplifiedRPM),
			slog.Float64("expected_combined_rpm", summary.CombinedRPM),
			slog.Float64("expected_combined_retry_amplified_rpm_max", summary.CombinedRetryAmplifiedRPM),
			slog.Float64("budget_rpm", summary.BudgetRPM),
		)
	}
}

func estimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
	var rpm float64
	for _, registration := range registrations {
		rpm += estimateRegistrationRPM(registration)
	}
	return rpm
}

func estimateRegistrationRPM(registration providers.ChannelPollerRegistration) float64 {
	if registration.Poller == nil || registration.Interval <= 0 {
		return 0
	}
	channelCount := resolvedRegistrationChannelCount(registration)
	if channelCount == 0 {
		return 0
	}
	requestsPerRun := resolvedRegistrationRequestsPerRun(registration)
	return float64(channelCount) * float64(requestsPerRun) * (60.0 / registration.Interval.Seconds())
}

func estimateRegistrationWorstCaseRPM(registration providers.ChannelPollerRegistration) float64 {
	if registration.WorstCaseRequestUnitsPerRun > 0 {
		if registration.Poller == nil || registration.Interval <= 0 {
			return 0
		}
		channelCount := resolvedRegistrationChannelCount(registration)
		if channelCount == 0 {
			return 0
		}
		return float64(channelCount) * registration.WorstCaseRequestUnitsPerRun * (60.0 / registration.Interval.Seconds())
	}
	attempts := registration.WorstCaseAttempts
	if attempts <= 0 {
		attempts = scraper.FetchPageMaxAttempts
	}
	return estimateRegistrationRPM(registration) * float64(attempts)
}

func resolvedRegistrationChannelCount(registration providers.ChannelPollerRegistration) int {
	return len(mergeUniqueChannelIDs(registration.ChannelIDs))
}

func resolvedRegistrationRequestsPerRun(registration providers.ChannelPollerRegistration) int {
	if registration.RequestsPerRun <= 0 {
		return 1
	}
	return registration.RequestsPerRun
}

func isPublishedAtResolverRegistration(registration providers.ChannelPollerRegistration) bool {
	return registration.Poller != nil && registration.Poller.Name() == poller.PendingPublishedAtResolverPollerName
}

func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	missing := make([]string, 0)
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if registration.HasExplicitChannelIDs {
			continue
		}
		missing = append(missing, registration.Poller.Name())
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"stream-ingester poller registrations require explicit channel IDs: %s",
		strings.Join(missing, ", "),
	)
}
