package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert alarm state batch: db is nil")
	}

	normalized, err := normalizeAlarmStateBatchRecords(records)
	if err != nil {
		return err
	}
	now := yttimestamp.Normalize(time.Now())
	finalAuthorizedExpr := `CASE
                WHEN youtube_community_shorts_alarm_states.authorized_at IS NULL THEN EXCLUDED.authorized_at
                WHEN EXCLUDED.authorized_at IS NULL THEN youtube_community_shorts_alarm_states.authorized_at
                WHEN EXCLUDED.authorized_at < youtube_community_shorts_alarm_states.authorized_at THEN EXCLUDED.authorized_at
                ELSE youtube_community_shorts_alarm_states.authorized_at
            END`
	finalAlarmSentExpr := `CASE
                WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
                WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_community_shorts_alarm_states.alarm_sent_at
                WHEN EXCLUDED.alarm_sent_at < youtube_community_shorts_alarm_states.alarm_sent_at THEN EXCLUDED.alarm_sent_at
                ELSE youtube_community_shorts_alarm_states.alarm_sent_at
            END`
	deliveryStatusExpr := buildAlarmStateDeliveryStatusExpr(finalAuthorizedExpr, finalAlarmSentExpr)
	query, args := buildAlarmStateUpsertQuery(normalized, now, finalAuthorizedExpr, finalAlarmSentExpr, deliveryStatusExpr)
	if err := r.db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		return fmt.Errorf("upsert alarm state batch: exec query: %w", err)
	}

	return nil
}

func normalizeAlarmStateBatchRecords(records []*domain.YouTubeCommunityShortsAlarmState) ([]*domain.YouTubeCommunityShortsAlarmState, error) {
	normalizedByIdentity := make(map[string]*domain.YouTubeCommunityShortsAlarmState, len(records))
	normalizedOrder := make([]string, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeAlarmState(record)
		if err != nil {
			return nil, fmt.Errorf("upsert alarm state batch: normalize record at index %d: %w", i, err)
		}

		identityKey := alarmStateCanonicalKey(normalizedRecord.Kind, normalizedRecord.PostID)
		if existing, ok := normalizedByIdentity[identityKey]; ok {
			normalizedByIdentity[identityKey] = mergeNormalizedAlarmState(existing, normalizedRecord)
			continue
		}

		normalizedByIdentity[identityKey] = normalizedRecord
		normalizedOrder = append(normalizedOrder, identityKey)
	}

	normalized := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(normalizedOrder))
	for _, identityKey := range normalizedOrder {
		normalized = append(normalized, normalizedByIdentity[identityKey])
	}

	return normalized, nil
}

func buildAlarmStateUpsertQuery(
	normalized []*domain.YouTubeCommunityShortsAlarmState,
	now time.Time,
	finalAuthorizedExpr string,
	finalAlarmSentExpr string,
	deliveryStatusExpr string,
) (string, []any) {
	args := make([]any, 0, len(normalized)*11)
	var sb strings.Builder
	sb.WriteString(`
        INSERT INTO youtube_community_shorts_alarm_states
            (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
        VALUES
    `)
	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			record.Kind,
			record.PostID,
			record.ContentID,
			record.ChannelID,
			record.ActualPublishedAt,
			record.DetectedAt,
			record.AuthorizedAt,
			record.AlarmSentAt,
			record.DeliveryStatus,
			now,
			now,
		)
	}
	sb.WriteString(`
        ON CONFLICT (kind, post_id) DO UPDATE
        SET content_id = EXCLUDED.content_id,
            channel_id = EXCLUDED.channel_id,
            actual_published_at = COALESCE(youtube_community_shorts_alarm_states.actual_published_at, EXCLUDED.actual_published_at),
            detected_at = CASE
                WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
                ELSE youtube_community_shorts_alarm_states.detected_at
            END,
            authorized_at = `)
	sb.WriteString(finalAuthorizedExpr)
	sb.WriteString(`,
            alarm_sent_at = `)
	sb.WriteString(finalAlarmSentExpr)
	sb.WriteString(`,
            delivery_status = `)
	sb.WriteString(deliveryStatusExpr)
	sb.WriteString(`,
            updated_at = EXCLUDED.updated_at
    `)
	return sb.String(), args
}

func normalizeAlarmStateClaim(record *domain.YouTubeCommunityShortsAlarmState) (*domain.YouTubeCommunityShortsAlarmState, error) {
	normalizedRecord, err := normalizeAlarmState(record)
	if err != nil {
		return nil, err
	}
	expectedPostID := canonicalTrackingIdentity(normalizedRecord.Kind, normalizedRecord.ContentID)
	if expectedPostID != normalizedRecord.PostID {
		return nil, fmt.Errorf("post id/content id mismatch")
	}
	if normalizedRecord.AuthorizedAt == nil || normalizedRecord.AuthorizedAt.IsZero() {
		return nil, fmt.Errorf("authorized_at is empty")
	}
	if normalizedRecord.AlarmSentAt != nil && !normalizedRecord.AlarmSentAt.IsZero() {
		return nil, fmt.Errorf("alarm_sent_at must be empty")
	}

	authorizedAt := normalizeDatabaseTimestamp(*normalizedRecord.AuthorizedAt)
	normalizedRecord.AuthorizedAt = &authorizedAt
	normalizedRecord.AlarmSentAt = nil
	normalizedRecord.DeliveryStatus = domain.YouTubeCommunityShortsAlarmStateStatusEnqueued
	return normalizedRecord, nil
}

func normalizeAlarmState(record *domain.YouTubeCommunityShortsAlarmState) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(record.Kind, record.PostID)
	if err != nil {
		return nil, err
	}
	_, normalizedContentID, err := normalizeIdentity(record.Kind, record.ContentID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if record.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected_at is empty")
	}

	actualPublishedAt := yttimestamp.NormalizePtr(record.ActualPublishedAt)
	authorizedAt := yttimestamp.NormalizePtr(record.AuthorizedAt)
	alarmSentAt := yttimestamp.NormalizePtr(record.AlarmSentAt)

	return &domain.YouTubeCommunityShortsAlarmState{
		Kind:              normalizedKind,
		PostID:            normalizedPostID,
		ContentID:         normalizedContentID,
		ChannelID:         normalizedChannelID,
		ActualPublishedAt: actualPublishedAt,
		DetectedAt:        yttimestamp.Normalize(record.DetectedAt),
		AuthorizedAt:      authorizedAt,
		AlarmSentAt:       alarmSentAt,
		DeliveryStatus:    domain.ResolveYouTubeCommunityShortsAlarmStateStatus(authorizedAt, alarmSentAt),
	}, nil
}

func normalizeDatabaseTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func alarmStateCanonicalKey(kind domain.OutboxKind, postID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(postID)
}

func mergeNormalizedAlarmState(existing *domain.YouTubeCommunityShortsAlarmState, next *domain.YouTubeCommunityShortsAlarmState) *domain.YouTubeCommunityShortsAlarmState {
	if existing == nil {
		return next
	}
	if next == nil {
		return existing
	}

	merged := *existing
	if strings.TrimSpace(next.ContentID) != "" {
		merged.ContentID = next.ContentID
	}
	if strings.TrimSpace(next.ChannelID) != "" {
		merged.ChannelID = next.ChannelID
	}
	if next.ActualPublishedAt != nil {
		merged.ActualPublishedAt = next.ActualPublishedAt
	}
	if next.DetectedAt.Before(merged.DetectedAt) {
		merged.DetectedAt = next.DetectedAt
	}
	merged.AuthorizedAt = earliestAlarmStateTimestamp(merged.AuthorizedAt, next.AuthorizedAt)
	merged.AlarmSentAt = earliestAlarmStateTimestamp(merged.AlarmSentAt, next.AlarmSentAt)
	merged.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(merged.AuthorizedAt, merged.AlarmSentAt)

	return &merged
}

func earliestAlarmStateTimestamp(existing *time.Time, next *time.Time) *time.Time {
	if existing == nil {
		return next
	}
	if next != nil && next.Before(*existing) {
		return next
	}
	return existing
}

func buildAlarmStateDeliveryStatusExpr(authorizedExpr string, alarmSentExpr string) string {
	return fmt.Sprintf(`CASE
                WHEN (%s) IS NOT NULL THEN '%s'
                WHEN (%s) IS NOT NULL THEN '%s'
                ELSE '%s'
            END`,
		alarmSentExpr,
		domain.YouTubeCommunityShortsAlarmStateStatusSent,
		authorizedExpr,
		domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	)
}
