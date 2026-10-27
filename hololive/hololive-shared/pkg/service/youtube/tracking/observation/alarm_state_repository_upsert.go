package observation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *alarmStateRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	if len(records) == 0 {
		return nil
	}

	if r == nil || r.db == nil {
		return errors.New("upsert alarm state batch: db is nil")
	}

	normalized, err := normalizeAlarmStateBatchRecords(records)
	if err != nil {
		return fmt.Errorf("normalize alarm state batch records: %w", err)
	}

	now := yttimestamp.Normalize(time.Now())
	finalAlarmSentExpr := `CASE
                WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
                WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_community_shorts_alarm_states.alarm_sent_at
                WHEN EXCLUDED.alarm_sent_at < youtube_community_shorts_alarm_states.alarm_sent_at THEN EXCLUDED.alarm_sent_at
                ELSE youtube_community_shorts_alarm_states.alarm_sent_at
            END`
	query, args := buildAlarmStateUpsertQuery(normalized, now, finalAlarmSentExpr)

	if _, err := dbx.ExecSQL(ctx, r.db, "upsert alarm state batch: exec query", query, args...); err != nil {
		return fmt.Errorf("exec SQL: %w", err)
	}

	return nil
}

func normalizeAlarmStateBatchRecords(records []*domain.YouTubeCommunityShortsAlarmState) ([]*domain.YouTubeCommunityShortsAlarmState, error) {
	return normalizeBatchRecords(records, "upsert alarm state batch", normalizeAlarmState,
		func(record *domain.YouTubeCommunityShortsAlarmState) string {
			return alarmStateCanonicalKey(record.Kind, record.PostID)
		}, mergeNormalizedAlarmState)
}

func buildAlarmStateUpsertQuery(
	normalized []*domain.YouTubeCommunityShortsAlarmState,
	now time.Time,
	finalAlarmSentExpr string,
) (string, []any) {
	const columnsPerRow = 10

	args := make([]any, 0, len(normalized)*columnsPerRow)

	var sb strings.Builder

	sb.WriteString(mustSQL("alarm_state_repository_upsert_0084_01.sql"))

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}

		writeRowPlaceholders(&sb, i, columnsPerRow)

		args = append(args,
			record.Kind,
			record.PostID,
			record.ContentID,
			record.ChannelID,
			record.ActualPublishedAt,
			record.DetectedAt,
			record.AlarmSentAt,
			record.DeliveryStatus,
			now,
			now,
		)
	}

	sb.WriteString(mustSQL("alarm_state_repository_upsert_0108_02.sql"))
	sb.WriteString(finalAlarmSentExpr)
	sb.WriteString(`,
            delivery_status = `)
	sb.WriteString(buildAlarmStateDeliveryStatusExpr(finalAlarmSentExpr))
	sb.WriteString(`,
            updated_at = EXCLUDED.updated_at
    `)

	return sb.String(), args
}

func normalizeAlarmState(record *domain.YouTubeCommunityShortsAlarmState) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if record == nil {
		return nil, errors.New("record is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(record.Kind, record.PostID)
	if err != nil {
		return nil, fmt.Errorf("normalize source post identity: %w", err)
	}

	_, normalizedContentID, err := normalizeIdentity(record.Kind, record.ContentID)
	if err != nil {
		return nil, fmt.Errorf("normalize identity: %w", err)
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, errors.New("channel id is empty")
	}

	if record.DetectedAt.IsZero() {
		return nil, errors.New("detected_at is empty")
	}

	actualPublishedAt := yttimestamp.NormalizePtr(record.ActualPublishedAt)
	alarmSentAt := yttimestamp.NormalizePtr(record.AlarmSentAt)

	return &domain.YouTubeCommunityShortsAlarmState{
		Kind:              normalizedKind,
		PostID:            normalizedPostID,
		ContentID:         normalizedContentID,
		ChannelID:         normalizedChannelID,
		ActualPublishedAt: actualPublishedAt,
		DetectedAt:        yttimestamp.Normalize(record.DetectedAt),
		AlarmSentAt:       alarmSentAt,
		DeliveryStatus:    domain.ResolveYouTubeCommunityShortsAlarmStateStatus(alarmSentAt),
	}, nil
}

func alarmStateCanonicalKey(kind domain.OutboxKind, postID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(postID)
}

func mergeNormalizedAlarmState(existing, next *domain.YouTubeCommunityShortsAlarmState) *domain.YouTubeCommunityShortsAlarmState {
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

	merged.AlarmSentAt = earliestAlarmStateTimestamp(merged.AlarmSentAt, next.AlarmSentAt)
	merged.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(merged.AlarmSentAt)

	return &merged
}

func earliestAlarmStateTimestamp(existing, next *time.Time) *time.Time {
	if existing == nil {
		return next
	}

	if next != nil && next.Before(*existing) {
		return next
	}

	return existing
}

// buildAlarmStateDeliveryStatusExpr는 병합된 alarm_sent_at 하나로 delivery_status를 계산하는 SQL 식을 만든다.
func buildAlarmStateDeliveryStatusExpr(alarmSentExpr string) string {
	return fmt.Sprintf(`CASE
                WHEN (%s) IS NOT NULL THEN '%s'
                ELSE '%s'
            END`,
		alarmSentExpr,
		domain.YouTubeCommunityShortsAlarmStateStatusSent,
		domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	)
}
