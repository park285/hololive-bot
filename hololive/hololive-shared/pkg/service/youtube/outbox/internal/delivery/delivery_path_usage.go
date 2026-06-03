package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/analytics"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type PostDeliveryPathUsage = analytics.PostDeliveryPathUsage

type postDeliveryPathUsageScanRow struct {
	OutboxKind         domain.OutboxKind `db:"outbox_kind"`
	AlarmType          domain.AlarmType  `db:"alarm_type"`
	ChannelID          string            `db:"channel_id"`
	PostID             string            `db:"post_id"`
	ContentID          string            `db:"content_id"`
	DeliveryPath       string            `db:"delivery_path"`
	ActualPublishedAt  scannableTime     `db:"actual_published_at"`
	DetectedAt         scannableTime     `db:"detected_at"`
	FirstEventAt       scannableTime     `db:"first_event_at"`
	LastEventAt        scannableTime     `db:"last_event_at"`
	FirstSuccessAt     scannableTime     `db:"first_success_at"`
	LastSuccessAt      scannableTime     `db:"last_success_at"`
	SuccessSendCount   int64             `db:"success_send_count"`
	SuccessRoomCount   int64             `db:"success_room_count"`
	FailedAttemptCount int64             `db:"failed_attempt_count"`
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryPathUsageSince(ctx context.Context, since time.Time) ([]PostDeliveryPathUsage, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery path usage since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post delivery path usage since: since is empty")
	}

	var scanned []postDeliveryPathUsageScanRow
	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}
	query := `
		SELECT ` + strings.Join([]string{
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
	}, ", ") + `
		FROM youtube_content_alarm_tracking AS track
		LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id
		LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?
		WHERE ` + deliverysql.DeliveryInClause("track.kind", len(postKinds)) + `
		  AND COALESCE(track.actual_published_at, track.detected_at) >= ?
		GROUP BY ` + strings.Join([]string{
		"track.kind",
		"track.channel_id",
		"track.content_id",
		"COALESCE(t.delivery_path, '')",
		"track.actual_published_at",
		"track.detected_at",
	}, ", ") + `
		ORDER BY track.channel_id ASC, track.content_id ASC, COALESCE(t.delivery_path, '') ASC
	`
	args := []any{since.UTC()}
	args = deliverysql.AppendDeliveryOutboxKindArgs(args, postKinds...)
	args = append(args, since.UTC())
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &scanned, "list post delivery path usage since: scan rows", query, args...); err != nil {
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
