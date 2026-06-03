package telemetry

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/analytics"
)

type PostLatencyPeriod = analytics.PostLatencyPeriod
type PostLatencyPeriodSummary = analytics.PostLatencyPeriodSummary

func (r *Repository) ListPostLatencyPeriodSummaries(ctx context.Context, periods []PostLatencyPeriod) ([]PostLatencyPeriodSummary, error) {
	normalizedPeriods, err := analytics.NormalizePostLatencyPeriods(periods)
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: %w", err)
	}
	if len(normalizedPeriods) == 0 {
		return []PostLatencyPeriodSummary{}, nil
	}

	posts, err := r.ListPostSendCountsSince(ctx, analytics.EarliestPostLatencyPeriodStart(normalizedPeriods))
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: load post send counts: %w", err)
	}

	summaries, err := analytics.BuildPostLatencyPeriodSummaries(posts, normalizedPeriods)
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: %w", err)
	}

	return summaries, nil
}
