package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const communityShortsDeliveryLogMaxLimit = 5000

func (r *DeliveryTelemetryRepository) ListCommunityShortsDeliveryLogsSince(
	ctx context.Context,
	since time.Time,
	limit int,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list community shorts delivery logs since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list community shorts delivery logs since: since is empty")
	}

	normalizedLimit := normalizeCommunityShortsDeliveryLogLimit(limit)
	query := r.db.WithContext(ctx).
		Where("alarm_type IN ?", []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}).
		Where("COALESCE(actual_published_at, detected_at, event_at) >= ?", since.UTC()).
		Order("COALESCE(actual_published_at, detected_at, event_at) DESC").
		Order("event_at ASC").
		Order("id ASC")
	if normalizedLimit > 0 {
		query = query.Limit(normalizedLimit)
	}

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list community shorts delivery logs since: query rows: %w", err)
	}

	return rows, nil
}

func normalizeCommunityShortsDeliveryLogLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > communityShortsDeliveryLogMaxLimit {
		return communityShortsDeliveryLogMaxLimit
	}
	return limit
}
