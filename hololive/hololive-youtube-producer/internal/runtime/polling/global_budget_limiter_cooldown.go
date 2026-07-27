package polling

import (
	"context"
	"fmt"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
)

func (l *globalBudgetLimiter) fallbackSourceCooldownDecision(ctx context.Context, profile polling.BudgetProfile) (decision polling.BudgetDecision, denied bool, err error) {
	for _, source := range positiveFallbackBudgetSources(profile) {
		decision, denied, err := l.sourceCooldownDecision(ctx, source, profile.BurstClass)
		if err != nil || denied {
			return decision, denied, err
		}
	}
	return polling.BudgetDecision{}, false, nil
}

func positiveFallbackBudgetSources(profile polling.BudgetProfile) []polling.BudgetSource {
	sources := sortedBudgetSources(profile.FallbackSourceUnits)
	filtered := sources[:0]
	for _, source := range sources {
		if profile.FallbackSourceUnits[source] > 0 {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

func (l *globalBudgetLimiter) sourceCooldownDecision(ctx context.Context, source polling.BudgetSource, class polling.BudgetBurstClass) (decision polling.BudgetDecision, denied bool, err error) {
	client := l.cacheClient.GetClient()
	if client == nil {
		return polling.BudgetDecision{}, false, fmt.Errorf("try reserve global budget: fallback source cooldown: cache client is nil")
	}
	keys := l.keys(source, class, "")
	ttlMS, err := client.Do(ctx, l.cacheClient.B().Pttl().Key(keys.SourceCooldown).Build()).AsInt64()
	if err != nil {
		return polling.BudgetDecision{}, false, fmt.Errorf("try reserve global budget: fallback source cooldown %s: %w", source, err)
	}
	if ttlMS == -2 {
		return polling.BudgetDecision{}, false, nil
	}
	return l.buildSourceCooldownDecision(source, ttlMS), true, nil
}

func (l *globalBudgetLimiter) buildSourceCooldownDecision(source polling.BudgetSource, ttlMS int64) polling.BudgetDecision {
	retryAfter := l.deniedRetryAfter
	if ttlMS >= 0 {
		retryAfter = millisDuration(ttlMS)
	}
	return polling.BudgetDecision{
		Allowed:        false,
		RetryAfter:     retryAfter,
		Reason:         "source_cooldown",
		AffectedSource: string(source),
	}
}
