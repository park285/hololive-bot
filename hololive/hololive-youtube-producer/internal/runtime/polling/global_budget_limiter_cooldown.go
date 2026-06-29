package polling

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func (l *globalBudgetLimiter) fallbackSourceCooldownDecision(ctx context.Context, profile poller.BudgetProfile) (decision poller.BudgetDecision, denied bool, err error) {
	for _, source := range positiveFallbackBudgetSources(profile) {
		decision, denied, err := l.sourceCooldownDecision(ctx, source, profile.BurstClass)
		if err != nil || denied {
			return decision, denied, err
		}
	}
	return poller.BudgetDecision{}, false, nil
}

func positiveFallbackBudgetSources(profile poller.BudgetProfile) []poller.BudgetSource {
	sources := sortedBudgetSources(profile.FallbackSourceUnits)
	filtered := sources[:0]
	for _, source := range sources {
		if profile.FallbackSourceUnits[source] > 0 {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

func (l *globalBudgetLimiter) sourceCooldownDecision(ctx context.Context, source poller.BudgetSource, class poller.BudgetBurstClass) (decision poller.BudgetDecision, denied bool, err error) {
	client := l.cacheClient.GetClient()
	if client == nil {
		return poller.BudgetDecision{}, false, fmt.Errorf("try reserve global budget: fallback source cooldown: cache client is nil")
	}
	keys := l.keys(source, class, "")
	ttlMS, err := client.Do(ctx, l.cacheClient.B().Pttl().Key(keys.SourceCooldown).Build()).AsInt64()
	if err != nil {
		return poller.BudgetDecision{}, false, fmt.Errorf("try reserve global budget: fallback source cooldown %s: %w", source, err)
	}
	if ttlMS == -2 {
		return poller.BudgetDecision{}, false, nil
	}
	return l.buildSourceCooldownDecision(source, ttlMS), true, nil
}

func (l *globalBudgetLimiter) buildSourceCooldownDecision(source poller.BudgetSource, ttlMS int64) poller.BudgetDecision {
	retryAfter := l.deniedRetryAfter
	if ttlMS >= 0 {
		retryAfter = millisDuration(ttlMS)
	}
	return poller.BudgetDecision{
		Allowed:        false,
		RetryAfter:     retryAfter,
		Reason:         "source_cooldown",
		AffectedSource: string(source),
	}
}
