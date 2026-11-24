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
	if _, err := dbx.ExecSQL(ctx, r.db, "upsert alarm state batch: exec query", upsertAlarmStateBatchSQL, alarmStateUpsertArgs(normalized, now)...); err != nil {
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

func alarmStateUpsertArgs(normalized []*domain.YouTubeCommunityShortsAlarmState, now time.Time) []any {
	size := len(normalized)
	kinds := make([]string, size)
	postIDs := make([]string, size)
	contentIDs := make([]string, size)
	channelIDs := make([]string, size)
	actualPublishedAts := make([]*time.Time, size)
	detectedAts := make([]time.Time, size)
	alarmSentAts := make([]*time.Time, size)
	deliveryStatuses := make([]string, size)

	for i, record := range normalized {
		kinds[i] = string(record.Kind)
		postIDs[i] = record.PostID
		contentIDs[i] = record.ContentID
		channelIDs[i] = record.ChannelID
		actualPublishedAts[i] = record.ActualPublishedAt
		detectedAts[i] = record.DetectedAt
		alarmSentAts[i] = record.AlarmSentAt
		deliveryStatuses[i] = string(record.DeliveryStatus)
	}

	return []any{kinds, postIDs, contentIDs, channelIDs, actualPublishedAts, detectedAts, alarmSentAts, deliveryStatuses, now}
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
