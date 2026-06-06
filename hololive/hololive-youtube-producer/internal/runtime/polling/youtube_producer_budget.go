package polling

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
)

type youtubeProducerBudgetSummary struct {
	PollerRPM                 float64
	PollerRetryAmplifiedRPM   float64
	ResolverRPM               float64
	ResolverRetryAmplifiedRPM float64
	CombinedRPM               float64
	CombinedRetryAmplifiedRPM float64
	BudgetRPM                 float64
}

func summarizeYouTubeProducerBudget(registrations []providers.ChannelPollerRegistration) youtubeProducerBudgetSummary {
	return summarizeYouTubeProducerBudgetWithLimit(registrations, defaultYouTubeProducerBudgetRPM())
}

func summarizeYouTubeProducerBudgetWithLimit(registrations []providers.ChannelPollerRegistration, budgetRPM float64) youtubeProducerBudgetSummary {
	pollerRPM := 0.0
	pollerRetryAmplifiedRPM := 0.0
	resolverRPM := 0.0
	resolverRetryAmplifiedRPM := 0.0
	for _, registration := range registrations {
		rpm := estimateRegistrationYouTubeScraperRPM(registration)
		retryAmplifiedRPM := estimateRegistrationYouTubeScraperWorstCaseRPM(registration)
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
	if budgetRPM <= 0 {
		budgetRPM = defaultYouTubeProducerBudgetRPM()
	}

	return youtubeProducerBudgetSummary{
		PollerRPM:                 pollerRPM,
		PollerRetryAmplifiedRPM:   pollerRetryAmplifiedRPM,
		ResolverRPM:               resolverRPM,
		ResolverRetryAmplifiedRPM: resolverRetryAmplifiedRPM,
		CombinedRPM:               combinedRPM,
		CombinedRetryAmplifiedRPM: combinedRetryAmplifiedRPM,
		BudgetRPM:                 budgetRPM,
	}
}

func defaultYouTubeProducerBudgetRPM() float64 {
	return 60.0 / config.DefaultYouTubeOperationalConfig().ProducerRequestInterval.Seconds()
}

func estimateRegistrationYouTubeScraperRPM(registration providers.ChannelPollerRegistration) float64 {
	if registration.HasBudgetProfile && !registrationHasReservedSourceUnits(registration, poller.BudgetSourceYouTubeScraper) {
		return 0
	}
	return estimateRegistrationRPM(registration)
}

func estimateRegistrationYouTubeScraperWorstCaseRPM(registration providers.ChannelPollerRegistration) float64 {
	if fallbackUnits := registrationFallbackSourceUnits(registration, poller.BudgetSourceYouTubeScraper); fallbackUnits > 0 {
		if registration.Poller == nil || registration.Interval <= 0 {
			return 0
		}
		channelCount := resolvedRegistrationChannelCount(registration)
		if channelCount == 0 {
			return 0
		}
		return float64(channelCount) * fallbackUnits * (60.0 / registration.Interval.Seconds())
	}
	if registration.HasBudgetProfile && !registrationHasReservedSourceUnits(registration, poller.BudgetSourceYouTubeScraper) {
		return 0
	}
	return estimateRegistrationWorstCaseRPM(registration)
}

func registrationHasReservedSourceUnits(registration providers.ChannelPollerRegistration, source poller.BudgetSource) bool {
	if len(registration.BudgetProfile.SourceUnits) == 0 {
		return false
	}
	units := registration.BudgetProfile.SourceUnits[source]
	return units > 0
}

func registrationFallbackSourceUnits(registration providers.ChannelPollerRegistration, source poller.BudgetSource) float64 {
	if len(registration.BudgetProfile.FallbackSourceUnits) == 0 {
		return 0
	}
	return registration.BudgetProfile.FallbackSourceUnits[source]
}

func validateYouTubeProducerPollerBudget(summary youtubeProducerBudgetSummary) error {
	return validateYouTubeProducerAggregateBudget(summary)
}

func logYouTubeProducerBudgetSummary(summary youtubeProducerBudgetSummary, logger *slog.Logger) {
	if logger == nil {
		return
	}

	logger.Info("youtube_producer_combined_budget_summary",
		slog.Float64("expected_poller_rpm", summary.PollerRPM),
		slog.Float64("expected_poller_retry_amplified_rpm_max", summary.PollerRetryAmplifiedRPM),
		slog.Float64("expected_resolver_rpm", summary.ResolverRPM),
		slog.Float64("expected_resolver_retry_amplified_rpm_max", summary.ResolverRetryAmplifiedRPM),
		slog.Float64("expected_combined_rpm", summary.CombinedRPM),
		slog.Float64("expected_combined_retry_amplified_rpm_max", summary.CombinedRetryAmplifiedRPM),
		slog.Float64("budget_rpm", summary.BudgetRPM),
	)
	if summary.CombinedRPM > summary.BudgetRPM {
		logger.Warn("youtube_producer_combined_budget_exceeds_rate_limit",
			slog.Float64("expected_poller_rpm", summary.PollerRPM),
			slog.Float64("expected_resolver_rpm", summary.ResolverRPM),
			slog.Float64("expected_combined_rpm", summary.CombinedRPM),
			slog.Float64("budget_rpm", summary.BudgetRPM),
		)
	}
	if summary.CombinedRetryAmplifiedRPM > summary.BudgetRPM {
		logger.Warn("youtube_producer_fault_envelope_exceeds_rate_limit",
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
	return len(polltarget.MergeUniqueChannelIDs(registration.ChannelIDs))
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
		"youtube-producer poller registrations require explicit channel IDs: %s",
		strings.Join(missing, ", "),
	)
}

func youtubeScraperBudgetProfile(units float64, class poller.BudgetBurstClass, priority poller.BudgetPriority) poller.BudgetProfile {
	return poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceYouTubeScraper: units,
			poller.BudgetSourcePostgresWrite:  1,
		},
		BurstClass: class,
		Priority:   priority,
	}
}

func budgetProfileWithRegistrationPriority(profile poller.BudgetProfile, priority poller.Priority) poller.BudgetProfile {
	profile.Priority = budgetPriorityFromRegistrationPriority(priority)
	return profile
}

func budgetPriorityFromRegistrationPriority(priority poller.Priority) poller.BudgetPriority {
	switch priority {
	case poller.PriorityHigh, poller.PriorityBoost:
		return poller.BudgetPriorityHigh
	case poller.PriorityLow:
		return poller.BudgetPriorityLow
	default:
		return poller.BudgetPriorityNormal
	}
}
