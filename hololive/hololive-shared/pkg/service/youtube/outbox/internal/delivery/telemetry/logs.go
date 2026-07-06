package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

const communityShortsDeliveryLogMaxLimit = 5000

func (r *Repository) ListCommunityShortsDeliveryLogsSince(
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
	alarmTypes := []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}
	query := mustSQL("logs_0028_01.sql") + deliveryTelemetrySelectColumns() + `
		FROM youtube_notification_delivery_telemetry
		WHERE ` + deliverysql.DeliveryInClause("alarm_type", len(alarmTypes)) + `
		  AND COALESCE(actual_published_at, detected_at, event_at) >= ?
		ORDER BY COALESCE(actual_published_at, detected_at, event_at) DESC, event_at ASC, id ASC
	`
	args := deliverysql.AppendDeliveryAlarmTypeArgs(nil, alarmTypes...)
	args = append(args, since.UTC())
	if normalizedLimit > 0 {
		query += " LIMIT ?"
		args = append(args, normalizedLimit)
	}

	rows, err := r.queryTelemetryRows(ctx, "list community shorts delivery logs since: query rows", deliverysql.PostgresPlaceholders(query), args...)
	if err != nil {
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
