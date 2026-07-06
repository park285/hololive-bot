package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *identityRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find tracking by identity: db is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(kind, contentID)
	if err != nil {
		return nil, fmt.Errorf("find tracking by identity: %w", err)
	}

	candidates := trackingIdentityCandidates(normalizedKind, normalizedContentID)
	preferredContentID := canonicalTrackingIdentity(normalizedKind, normalizedContentID)

	records, err := r.findByIdentityRecords(ctx, normalizedKind, preferredContentID, candidates)
	if err != nil {
		return nil, err
	}

	return preferTrackingIdentityRecord(records, preferredContentID), nil
}

func (r *identityRepository) findByIdentityRecords(
	ctx context.Context,
	normalizedKind domain.OutboxKind,
	preferredContentID string,
	candidates []string,
) ([]domain.YouTubeContentAlarmTracking, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	values, args := buildIdentityLookupValues(normalizedKind, preferredContentID, candidates)
	var records []domain.YouTubeContentAlarmTracking
	query := `
		WITH input(kind, preferred_content_id, candidate_content_id) AS (
			VALUES ` + values + mustSQL("repository_identity_0049_01.sql")
	if err := dbx.SelectSQL(ctx, r.db, &records, "find tracking by identity: query row", query, args...); err != nil {
		return nil, err
	}

	return records, nil
}

func buildIdentityLookupValues(
	normalizedKind domain.OutboxKind,
	preferredContentID string,
	candidates []string,
) (query string, args []any) {
	args = make([]any, 0, len(candidates)*3)
	var values strings.Builder
	for i := range candidates {
		if i > 0 {
			values.WriteByte(',')
		}
		values.WriteString("(?, ?, ?)")
		args = append(args, normalizedKind, preferredContentID, candidates[i])
	}
	return values.String(), args
}

func preferTrackingIdentityRecord(
	records []domain.YouTubeContentAlarmTracking,
	preferredContentID string,
) *domain.YouTubeContentAlarmTracking {
	if len(records) == 0 {
		return nil
	}
	for i := range records {
		if strings.TrimSpace(records[i].ContentID) == preferredContentID {
			return &records[i]
		}
	}
	for i := range records {
		if strings.TrimSpace(records[i].CanonicalContentID) == preferredContentID {
			return &records[i]
		}
	}

	return &records[0]
}

func (r *identityRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	if record == nil {
		return fmt.Errorf("upsert tracking: record is nil")
	}
	return r.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{record})
}

func (r *identityRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
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
	latencyMillisExpr := buildLatencyMillisExpr(finalActualPublishedExpr, finalAlarmSentExpr)
	latencyExceededExpr := buildLatencyExceededExpr(latencyMillisExpr)
	deliveryStatusExpr := buildDeliveryStatusExpr(finalAlarmSentExpr)
	query, args := buildTrackingUpsertQuery(normalized, now, latencyMillisExpr, latencyExceededExpr, deliveryStatusExpr)
	if _, err := dbx.ExecSQL(ctx, r.db, "upsert tracking batch: exec query", query, args...); err != nil {
		return err
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
) (result1 string, result2 []any) {
	args := make([]any, 0, len(normalized)*12)
	var sb strings.Builder
	sb.WriteString(mustSQL("repository_identity_0183_02.sql"))
	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = appendTrackingUpsertValues(args, record, now)
	}
	sb.WriteString(mustSQL("repository_identity_0195_03.sql"))
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

func appendTrackingUpsertValues(args []any, record *domain.YouTubeContentAlarmTracking, now time.Time) []any {
	return append(args,
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
