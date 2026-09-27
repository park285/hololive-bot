package telemetry

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// scanTelemetryRow의 열 순서는 queries/repository_fetch_lock_pending.sql의 RETURNING 목록과 같아야 한다.
func scanTelemetryRow(row pgx.CollectableRow) (domain.YouTubeNotificationDeliveryTelemetry, error) {
	var (
		item                                              domain.YouTubeNotificationDeliveryTelemetry
		postID                                            sql.NullString
		dedupeKey, deliveryPath, deliveryMode, sendResult sql.NullString
		failureReason, rowError                           sql.NullString
	)

	err := row.Scan(
		&item.ID, &item.DeliveryID, &item.AttemptOrdinal, &item.OutboxID, &item.ChannelID, &item.ContentID, &postID, &item.RoomID, &item.AlarmType,
		&item.ActualPublishedAt, &item.AlarmSentAt, &item.AlarmLatencyMillis, &item.DetectedAt,
		&dedupeKey, &deliveryPath, &deliveryMode, &sendResult, &failureReason,
		&item.AttemptStartedAt, &item.AttemptFinishedAt, &item.EventAt, &item.NextAttemptAt, &item.CreatedAt, &item.LockedAt, &item.LoggedAt, &rowError,
	)

	item.PostID = nullStringValue(postID)
	item.DedupeKey = nullStringValue(dedupeKey)
	item.DeliveryPath = nullStringValue(deliveryPath)
	item.DeliveryMode = nullStringValue(deliveryMode)
	item.SendResult = nullStringValue(sendResult)
	item.FailureReason = nullStringValue(failureReason)
	item.Error = nullStringValue(rowError)

	if err != nil {
		return item, fmt.Errorf("scan telemetry row: %w", err)
	}

	return item, nil
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}

	return value.String
}
