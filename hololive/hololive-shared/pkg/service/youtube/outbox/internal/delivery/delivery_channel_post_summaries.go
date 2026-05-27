package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/analytics"
)

type ChannelPostDeliverySummary = analytics.ChannelPostDeliverySummary

var BuildChannelPostDeliverySummaries = analytics.BuildChannelPostDeliverySummaries

func (r *DeliveryTelemetryRepository) ListChannelPostDeliverySummariesSince(
	ctx context.Context,
	since time.Time,
) ([]ChannelPostDeliverySummary, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list channel post delivery summaries since: since is empty")
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
