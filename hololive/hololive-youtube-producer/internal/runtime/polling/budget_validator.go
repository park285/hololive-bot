package polling

import (
	"fmt"
	"log/slog"
	"strings"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
)

type BudgetEstimate struct {
	SustainedRPMBySource  map[polling.BudgetSource]float64
	BurstInflightBySource map[polling.BudgetSource]int
}

func validateRegistrationBudgetProfiles(registrations []providers.ChannelPollerRegistration) error {
	missing := make([]string, 0)
	for i := range registrations {
		registration := &registrations[i]
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if registration.HasBudgetProfile && len(registration.BudgetProfile.SourceUnits) > 0 {
			continue
		}
		missing = append(missing, registration.Poller.Name())
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"youtube-producer poller registrations require budget profiles: %s",
		strings.Join(missing, ", "),
	)
}

func estimateYouTubeProducerSourceBudget(registrations []providers.ChannelPollerRegistration, activeAPCount, perAPWorkerCount int) BudgetEstimate {
	estimate := BudgetEstimate{
		SustainedRPMBySource:  make(map[polling.BudgetSource]float64),
		BurstInflightBySource: make(map[polling.BudgetSource]int),
	}
	burstInflight := activeAPCount * perAPWorkerCount
	for i := range registrations {
		registration := &registrations[i]
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if len(registration.BudgetProfile.SourceUnits) == 0 {
			continue
		}
		accumulateRegistrationSourceBudget(&estimate, registration, burstInflight)
	}
	return estimate
}

func accumulateRegistrationSourceBudget(estimate *BudgetEstimate, registration *providers.ChannelPollerRegistration, burstInflight int) {
	channelCount := resolvedRegistrationChannelCount(registration)
	for source, units := range registration.BudgetProfile.SourceUnits {
		if channelCount > 0 {
			estimate.SustainedRPMBySource[source] += float64(channelCount) * units * (60.0 / registration.Interval.Seconds())
		}
		estimate.BurstInflightBySource[source] = burstInflight
	}
}

func logYouTubeProducerSourceBudgetEstimate(estimate BudgetEstimate, logger *slog.Logger) {
	if logger == nil {
		return
	}
	for _, source := range estimateBudgetSources(estimate) {
		logger.Info("youtube_producer_source_budget_estimate",
			slog.String("source", string(source)),
			slog.Float64("expected_sustained_rpm", estimate.SustainedRPMBySource[source]),
			slog.Int("expected_burst_inflight_max", estimate.BurstInflightBySource[source]),
		)
	}
}

func resolveYouTubeProducerActiveAPCount(configured int, activeActiveEnabled bool) (int, error) {
	if configured > 0 {
		return configured, nil
	}
	if activeActiveEnabled {
		return 0, fmt.Errorf("resolve active AP count: active-active is enabled but active instance count is not configured; set the fleet AP count explicitly")
	}
	return 1, nil
}

func validateYouTubeProducerAggregateBudget(summary youtubeProducerBudgetSummary) error {
	if summary.CombinedRPM <= summary.BudgetRPM {
		return nil
	}
	return fmt.Errorf(
		"youtube-producer combined active scraper RPM %.3f exceeds YouTube producer budget %.3f; increase poll intervals or reduce target channels",
		summary.CombinedRPM,
		summary.BudgetRPM,
	)
}

func estimateBudgetSources(estimate BudgetEstimate) []polling.BudgetSource {
	sourceCount := len(estimate.SustainedRPMBySource) + len(estimate.BurstInflightBySource)
	seen := make(map[polling.BudgetSource]struct{}, sourceCount)
	sources := make([]polling.BudgetSource, 0, sourceCount)
	for source := range estimate.SustainedRPMBySource {
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	for source := range estimate.BurstInflightBySource {
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	sortBudgetSources(sources)
	return sources
}
