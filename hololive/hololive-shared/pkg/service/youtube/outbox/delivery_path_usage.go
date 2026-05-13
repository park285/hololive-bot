package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type PostDeliveryPathUsage struct {
	OutboxKind         domain.OutboxKind `gorm:"column:outbox_kind"`
	AlarmType          domain.AlarmType  `gorm:"column:alarm_type"`
	ChannelID          string            `gorm:"column:channel_id"`
	PostID             string            `gorm:"column:post_id"`
	ContentID          string            `gorm:"column:content_id"`
	DeliveryPath       string            `gorm:"column:delivery_path"`
	ActualPublishedAt  *time.Time        `gorm:"column:actual_published_at"`
	DetectedAt         *time.Time        `gorm:"column:detected_at"`
	FirstEventAt       *time.Time        `gorm:"column:first_event_at"`
	LastEventAt        *time.Time        `gorm:"column:last_event_at"`
	FirstSuccessAt     *time.Time        `gorm:"column:first_success_at"`
	LastSuccessAt      *time.Time        `gorm:"column:last_success_at"`
	SuccessSendCount   int64             `gorm:"column:success_send_count"`
	SuccessRoomCount   int64             `gorm:"column:success_room_count"`
	FailedAttemptCount int64             `gorm:"column:failed_attempt_count"`
}

type postDeliveryPathUsageScanRow struct {
	OutboxKind         domain.OutboxKind `gorm:"column:outbox_kind"`
	AlarmType          domain.AlarmType  `gorm:"column:alarm_type"`
	ChannelID          string            `gorm:"column:channel_id"`
	PostID             string            `gorm:"column:post_id"`
	ContentID          string            `gorm:"column:content_id"`
	DeliveryPath       string            `gorm:"column:delivery_path"`
	ActualPublishedAt  scannableTime     `gorm:"column:actual_published_at"`
	DetectedAt         scannableTime     `gorm:"column:detected_at"`
	FirstEventAt       scannableTime     `gorm:"column:first_event_at"`
	LastEventAt        scannableTime     `gorm:"column:last_event_at"`
	FirstSuccessAt     scannableTime     `gorm:"column:first_success_at"`
	LastSuccessAt      scannableTime     `gorm:"column:last_success_at"`
	SuccessSendCount   int64             `gorm:"column:success_send_count"`
	SuccessRoomCount   int64             `gorm:"column:success_room_count"`
	FailedAttemptCount int64             `gorm:"column:failed_attempt_count"`
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryPathUsageSince(ctx context.Context, since time.Time) ([]PostDeliveryPathUsage, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery path usage since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post delivery path usage since: since is empty")
	}

	var scanned []postDeliveryPathUsageScanRow
	query := r.db.WithContext(ctx).
		Table("youtube_content_alarm_tracking AS track").
		Select(strings.Join([]string{
			"track.kind AS outbox_kind",
			"CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
			"track.channel_id AS channel_id",
			"COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id",
			"track.content_id AS content_id",
			"COALESCE(t.delivery_path, '') AS delivery_path",
			"track.actual_published_at AS actual_published_at",
			"track.detected_at AS detected_at",
			"MIN(t.event_at) AS first_event_at",
			"MAX(t.event_at) AS last_event_at",
			"MIN(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS first_success_at",
			"MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS last_success_at",
			"COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count",
			"COUNT(DISTINCT CASE WHEN t.send_result = 'success' THEN t.room_id END) AS success_room_count",
			"COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count",
		}, ", ")).
		Joins("LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id").
		Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?", since.UTC()).
		Where("track.kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("COALESCE(track.actual_published_at, track.detected_at) >= ?", since.UTC()).
		Group(strings.Join([]string{
			"track.kind",
			"track.channel_id",
			"track.content_id",
			"COALESCE(t.delivery_path, '')",
			"track.actual_published_at",
			"track.detected_at",
		}, ", ")).
		Order("track.channel_id ASC").
		Order("track.content_id ASC").
		Order("COALESCE(t.delivery_path, '') ASC")
	if err := query.Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("list post delivery path usage since: scan rows: %w", err)
	}

	return buildPostDeliveryPathUsageRows(scanned), nil
}

func buildPostDeliveryPathUsageRows(scanned []postDeliveryPathUsageScanRow) []PostDeliveryPathUsage {
	rows := make([]PostDeliveryPathUsage, 0, len(scanned))
	for i := range scanned {
		rows = append(rows, PostDeliveryPathUsage{
			OutboxKind:         scanned[i].OutboxKind,
			AlarmType:          scanned[i].AlarmType,
			ChannelID:          scanned[i].ChannelID,
			PostID:             scanned[i].PostID,
			ContentID:          scanned[i].ContentID,
			DeliveryPath:       strings.TrimSpace(scanned[i].DeliveryPath),
			ActualPublishedAt:  scanned[i].ActualPublishedAt.Ptr(),
			DetectedAt:         scanned[i].DetectedAt.Ptr(),
			FirstEventAt:       scanned[i].FirstEventAt.Ptr(),
			LastEventAt:        scanned[i].LastEventAt.Ptr(),
			FirstSuccessAt:     scanned[i].FirstSuccessAt.Ptr(),
			LastSuccessAt:      scanned[i].LastSuccessAt.Ptr(),
			SuccessSendCount:   scanned[i].SuccessSendCount,
			SuccessRoomCount:   scanned[i].SuccessRoomCount,
			FailedAttemptCount: scanned[i].FailedAttemptCount,
		})
	}

	return rows
}
