package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find alarm state by post id: db is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return nil, fmt.Errorf("find alarm state by post id: %w", err)
	}

	var row domain.YouTubeCommunityShortsAlarmState
	result := r.db.WithContext(ctx).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Limit(1).
		Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("find alarm state by post id: query row: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return &row, nil
}

func (r *GormRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	if record == nil {
		return fmt.Errorf("upsert alarm state: record is nil")
	}
	return r.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{record})
}

func (r *GormRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("try claim alarm state: db is nil")
	}

	normalizedRecord, err := normalizeAlarmStateClaim(record)
	if err != nil {
		return false, fmt.Errorf("try claim alarm state: %w", err)
	}

	now := yttimestamp.Normalize(time.Now())
	result := r.db.WithContext(ctx).Exec(`
        INSERT INTO youtube_community_shorts_alarm_states
            (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (kind, post_id) DO UPDATE
        SET content_id = EXCLUDED.content_id,
            channel_id = EXCLUDED.channel_id,
            actual_published_at = CASE
                WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_alarm_states.actual_published_at
                ELSE EXCLUDED.actual_published_at
            END,
            detected_at = CASE
                WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
                ELSE youtube_community_shorts_alarm_states.detected_at
            END,
            authorized_at = EXCLUDED.authorized_at,
            delivery_status = EXCLUDED.delivery_status,
            updated_at = EXCLUDED.updated_at
        WHERE youtube_community_shorts_alarm_states.authorized_at IS NULL
          AND youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
    `,
		normalizedRecord.Kind,
		normalizedRecord.PostID,
		normalizedRecord.ContentID,
		normalizedRecord.ChannelID,
		normalizedRecord.ActualPublishedAt,
		normalizedRecord.DetectedAt,
		normalizedRecord.AuthorizedAt,
		nil,
		normalizedRecord.DeliveryStatus,
		now,
		now,
	)
	if result.Error != nil {
		return false, fmt.Errorf("try claim alarm state: exec query: %w", result.Error)
	}

	return result.RowsAffected > 0, nil
}

func (r *GormRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("release alarm state claim: db is nil")
	}
	if authorizedAt.IsZero() {
		return false, fmt.Errorf("release alarm state claim: authorized_at is empty")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return false, fmt.Errorf("release alarm state claim: %w", err)
	}

	updatedAt := yttimestamp.Normalize(time.Now())
	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Where("alarm_sent_at IS NULL").
		Where("authorized_at = ?", yttimestamp.Normalize(authorizedAt)).
		Updates(map[string]any{
			"authorized_at":   nil,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusDetected,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return false, fmt.Errorf("release alarm state claim: update row: %w", result.Error)
	}

	return result.RowsAffected > 0, nil
}

func (r *GormRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert alarm state batch: db is nil")
	}

	normalizedByIdentity := make(map[string]*domain.YouTubeCommunityShortsAlarmState, len(records))
	normalizedOrder := make([]string, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeAlarmState(record)
		if err != nil {
			return fmt.Errorf("upsert alarm state batch: normalize record at index %d: %w", i, err)
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

	now := yttimestamp.Normalize(time.Now())
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
		args = append(
			args,
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

	sb.WriteString(`
        ON CONFLICT (kind, post_id) DO UPDATE
        SET content_id = EXCLUDED.content_id,
            channel_id = EXCLUDED.channel_id,
            actual_published_at = CASE
                WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_alarm_states.actual_published_at
                ELSE EXCLUDED.actual_published_at
            END,
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

	if err := r.db.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("upsert alarm state batch: exec query: %w", err)
	}

	return nil
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
	switch {
	case merged.AuthorizedAt == nil:
		merged.AuthorizedAt = next.AuthorizedAt
	case next.AuthorizedAt != nil && next.AuthorizedAt.Before(*merged.AuthorizedAt):
		merged.AuthorizedAt = next.AuthorizedAt
	}
	switch {
	case merged.AlarmSentAt == nil:
		merged.AlarmSentAt = next.AlarmSentAt
	case next.AlarmSentAt != nil && next.AlarmSentAt.Before(*merged.AlarmSentAt):
		merged.AlarmSentAt = next.AlarmSentAt
	}
	merged.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(merged.AuthorizedAt, merged.AlarmSentAt)

	return &merged
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
