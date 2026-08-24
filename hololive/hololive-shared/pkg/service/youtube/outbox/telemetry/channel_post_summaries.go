package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
)

func (r *Repository) ListChannelPostDeliverySummariesSince(
	ctx context.Context,
	since time.Time,
) ([]analytics.ChannelPostDeliverySummary, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("list channel post delivery summaries since: db is nil")
	}

	if since.IsZero() {
		return nil, errors.New("list channel post delivery summaries since: since is empty")
	}

	posts, err := r.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: load post send counts: %w", err)
	}

	summaries, err := analytics.BuildChannelPostDeliverySummaries(posts)
	if err != nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: %w", err)
	}

	return summaries, nil
}
