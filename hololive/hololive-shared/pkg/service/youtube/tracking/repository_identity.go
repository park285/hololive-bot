package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find tracking by identity: db is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(kind, contentID)
	if err != nil {
		return nil, fmt.Errorf("find tracking by identity: %w", err)
	}

	candidates := trackingIdentityCandidates(normalizedKind, normalizedContentID)
	preferredContentID := canonicalTrackingIdentity(normalizedKind, normalizedContentID)

	var records []domain.YouTubeContentAlarmTracking
	query := r.db.WithContext(ctx).Where("kind = ?", normalizedKind)
	if len(candidates) == 1 {
		query = query.Where("(canonical_content_id = ? OR content_id = ?)", preferredContentID, candidates[0])
	} else {
		query = query.Where("(canonical_content_id = ? OR content_id IN ?)", preferredContentID, candidates)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("find tracking by identity: query row: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	for i := range records {
		if strings.TrimSpace(records[i].ContentID) == preferredContentID {
			return &records[i], nil
		}
	}
	for i := range records {
		if strings.TrimSpace(records[i].CanonicalContentID) == preferredContentID {
			return &records[i], nil
		}
	}

	return &records[0], nil
}

func (r *GormRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	if record == nil {
		return fmt.Errorf("upsert tracking: record is nil")
	}
	return r.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{record})
}

func (r *GormRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert tracking batch: db is nil")
	}

	normalized, err := normalizeTrackingBatchRecords(records)
	if err != nil {
		return err
	}
	now := timestamp.Normalize(time.Now())
	finalActualPublishedExpr := `CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_content_alarm_tracking.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END`
	finalAlarmSentExpr := `CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END`
	latencyMillisExpr := buildLatencyMillisExpr(r.db, finalActualPublishedExpr, finalAlarmSentExpr)
	latencyExceededExpr := buildLatencyExceededExpr(latencyMillisExpr)
	deliveryStatusExpr := buildDeliveryStatusExpr(finalAlarmSentExpr)
	query, args := buildTrackingUpsertQuery(normalized, now, latencyMillisExpr, latencyExceededExpr, deliveryStatusExpr)
	if err := r.db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		return fmt.Errorf("upsert tracking batch: exec query: %w", err)
	}

	return nil
}

func normalizeTrackingBatchRecords(records []*domain.YouTubeContentAlarmTracking) ([]*domain.YouTubeContentAlarmTracking, error) {
	normalizedByIdentity := make(map[string]*domain.YouTubeContentAlarmTracking, len(records))
	normalizedOrder := make([]string, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeRecord(record)
		if err != nil {
			return nil, fmt.Errorf("upsert tracking batch: normalize record at index %d: %w", i, err)
		}

		identityKey := trackingCanonicalKey(normalizedRecord.Kind, normalizedRecord.CanonicalContentID)
		if existing, ok := normalizedByIdentity[identityKey]; ok {
			normalizedByIdentity[identityKey] = mergeNormalizedTrackingRecord(existing, normalizedRecord)
			continue
		}

		normalizedByIdentity[identityKey] = normalizedRecord
		normalizedOrder = append(normalizedOrder, identityKey)
	}

	normalized := make([]*domain.YouTubeContentAlarmTracking, 0, len(normalizedOrder))
	for _, identityKey := range normalizedOrder {
		normalized = append(normalized, normalizedByIdentity[identityKey])
	}

	return normalized, nil
}

func buildTrackingUpsertQuery(
	normalized []*domain.YouTubeContentAlarmTracking,
	now time.Time,
	latencyMillisExpr string,
	latencyExceededExpr string,
	deliveryStatusExpr string,
) (string, []any) {
	args := make([]any, 0, len(normalized)*12)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, created_at, updated_at)
		VALUES
	`)
	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			record.Kind,
			record.ContentID,
			record.CanonicalContentID,
			record.ChannelID,
			record.ActualPublishedAt,
			record.DetectedAt,
			record.AlarmSentAt,
			record.AlarmLatencyMillis,
			record.AlarmLatencyExceeded,
			record.DeliveryStatus,
			now,
			now,
		)
	}
	sb.WriteString(`
		ON CONFLICT (kind, canonical_content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_content_alarm_tracking.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END,
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_content_alarm_tracking.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_content_alarm_tracking.detected_at
		    END,
		    alarm_sent_at = CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END,
		    alarm_latency_millis = `)
	sb.WriteString(latencyMillisExpr)
	sb.WriteString(`,
		    alarm_latency_exceeded = `)
	sb.WriteString(latencyExceededExpr)
	sb.WriteString(`,
		    delivery_status = `)
	sb.WriteString(deliveryStatusExpr)
	sb.WriteString(`,
		    updated_at = EXCLUDED.updated_at
	`)
	return sb.String(), args
}
