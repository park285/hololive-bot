package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark alarm sent batch: db is nil")
	}

	normalized, err := normalizeAlarmSentMarks(marks)
	if err != nil {
		return fmt.Errorf("mark alarm sent batch: %w", err)
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return NewRepository(tx).applyAlarmSentMarks(ctx, normalized)
	}); err != nil {
		return fmt.Errorf("mark alarm sent batch transaction: %w", err)
	}

	return nil
}

func (r *GormRepository) applyAlarmSentMarks(ctx context.Context, marks []AlarmSentMark) error {
	updatedAt := yttimestamp.Normalize(time.Now())
	for i, mark := range marks {
		if err := r.applyAlarmSentMark(ctx, mark, updatedAt); err != nil {
			return fmt.Errorf("update mark at index %d: %w", i, err)
		}
	}
	return nil
}

func (r *GormRepository) applyAlarmSentMark(ctx context.Context, mark AlarmSentMark, updatedAt time.Time) error {
	trackingRow, err := r.FindByIdentity(ctx, mark.Kind, mark.ContentID)
	if err != nil {
		return fmt.Errorf("load tracking row: %w", err)
	}
	latencyMillis, latencyExceeded := calculateLatencyResult(nil, &mark.AlarmSentAt)
	targetContentID := mark.ContentID
	if trackingRow != nil {
		targetContentID = trackingRow.ContentID
		latencyMillis, latencyExceeded = calculateLatencyResult(trackingRow.ActualPublishedAt, &mark.AlarmSentAt)
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", mark.Kind, targetContentID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ?", mark.AlarmSentAt).
		Updates(map[string]any{
			"alarm_sent_at":          mark.AlarmSentAt,
			"alarm_latency_millis":   nullableInt64Value(latencyMillis),
			"alarm_latency_exceeded": nullableBoolValue(latencyExceeded),
			"delivery_status":        domain.YouTubeContentAlarmDeliveryStatusSent,
			"updated_at":             updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	if !isCommunityShortsAlarmStateKind(mark.Kind) {
		return nil
	}

	postID := canonicalTrackingIdentity(mark.Kind, targetContentID)
	if trackingRow != nil && strings.TrimSpace(trackingRow.CanonicalContentID) != "" {
		postID = strings.TrimSpace(trackingRow.CanonicalContentID)
	}

	return r.applyAlarmStateSentMark(ctx, mark, postID, targetContentID, trackingRow, updatedAt)
}

func (r *GormRepository) applyAlarmStateSentMark(
	ctx context.Context,
	mark AlarmSentMark,
	postID string,
	targetContentID string,
	trackingRow *domain.YouTubeContentAlarmTracking,
	updatedAt time.Time,
) error {
	finalized, err := r.finalizeClaimedAlarmState(ctx, mark, postID, updatedAt)
	if err != nil || finalized {
		return err
	}

	stateRow, err := r.FindAlarmStateByPostID(ctx, mark.Kind, postID)
	if err != nil {
		return fmt.Errorf("load alarm state row: %w", err)
	}

	if stateRow != nil {
		return r.applyExistingAlarmStateSentMark(ctx, mark, postID, stateRow, updatedAt)
	}

	return r.applyMissingAlarmStateSentMark(ctx, mark, postID, targetContentID, trackingRow)
}

func (r *GormRepository) finalizeClaimedAlarmState(ctx context.Context, mark AlarmSentMark, postID string, updatedAt time.Time) (bool, error) {
	if mark.AuthorizedAt == nil {
		return false, nil
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", mark.Kind, postID).
		Where("authorized_at = ?", *mark.AuthorizedAt).
		Where("alarm_sent_at IS NULL").
		Updates(map[string]any{
			"authorized_at":   nil,
			"alarm_sent_at":   mark.AlarmSentAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return false, fmt.Errorf("finalize claimed alarm state: update row: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *GormRepository) applyExistingAlarmStateSentMark(ctx context.Context, mark AlarmSentMark, postID string, stateRow *domain.YouTubeCommunityShortsAlarmState, updatedAt time.Time) error {
	if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
		if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
			return fmt.Errorf("refresh existing sent alarm state row: %w", err)
		}
		return nil
	}
	if mark.AuthorizedAt != nil {
		return fmt.Errorf("finalize claimed alarm state: claim authorization mismatch")
	}
	if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
		return fmt.Errorf("mark existing alarm state row sent: %w", err)
	}
	return nil
}

func (r *GormRepository) applyMissingAlarmStateSentMark(ctx context.Context, mark AlarmSentMark, postID string, targetContentID string, trackingRow *domain.YouTubeContentAlarmTracking) error {
	if trackingRow == nil {
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: tracking row missing")
		}
		return nil
	}

	alarmSentAt := mark.AlarmSentAt
	if err := r.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              mark.Kind,
		PostID:            postID,
		ContentID:         targetContentID,
		ChannelID:         trackingRow.ChannelID,
		ActualPublishedAt: trackingRow.ActualPublishedAt,
		DetectedAt:        trackingRow.DetectedAt,
		AlarmSentAt:       &alarmSentAt,
	}); err != nil {
		return fmt.Errorf("upsert fallback alarm state row: %w", err)
	}

	return nil
}

func (r *GormRepository) updateAlarmStateSentRow(ctx context.Context, kind domain.OutboxKind, postID string, alarmSentAt time.Time, updatedAt time.Time) error {
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return err
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ? OR authorized_at IS NOT NULL", alarmSentAt).
		Updates(map[string]any{
			"authorized_at": nil,
			"alarm_sent_at": gorm.Expr(
				"CASE WHEN alarm_sent_at IS NULL OR alarm_sent_at > ? THEN ? ELSE alarm_sent_at END",
				alarmSentAt,
				alarmSentAt,
			),
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func normalizeAlarmSentMarks(marks []AlarmSentMark) ([]AlarmSentMark, error) {
	normalized := make([]AlarmSentMark, 0, len(marks))
	indexByIdentity := make(map[string]int, len(marks))

	for i, mark := range marks {
		normalizedMark, identity, err := normalizeAlarmSentMark(i, mark)
		if err != nil {
			return nil, err
		}
		if existingIndex, ok := indexByIdentity[identity]; ok {
			if err := mergeAlarmSentMark(&normalized[existingIndex], normalizedMark, identity, i); err != nil {
				return nil, err
			}
			continue
		}

		indexByIdentity[identity] = len(normalized)
		normalized = append(normalized, normalizedMark)
	}

	return normalized, nil
}

func normalizeAlarmSentMark(index int, mark AlarmSentMark) (AlarmSentMark, string, error) {
	normalizedKind, normalizedContentID, err := normalizeIdentity(mark.Kind, mark.ContentID)
	if err != nil {
		return AlarmSentMark{}, "", fmt.Errorf("normalize mark at index %d: %w", index, err)
	}
	if mark.AlarmSentAt.IsZero() {
		return AlarmSentMark{}, "", fmt.Errorf("normalize mark at index %d: alarm sent at is empty", index)
	}

	normalizedAuthorizedAt, err := normalizeAlarmSentAuthorizedAt(index, mark.AuthorizedAt)
	if err != nil {
		return AlarmSentMark{}, "", err
	}
	normalizedMark := AlarmSentMark{
		Kind:         normalizedKind,
		ContentID:    normalizedContentID,
		AlarmSentAt:  yttimestamp.Normalize(mark.AlarmSentAt),
		AuthorizedAt: normalizedAuthorizedAt,
	}
	return normalizedMark, alarmSentMarkIdentity(normalizedKind, normalizedContentID), nil
}

func normalizeAlarmSentAuthorizedAt(index int, authorizedAt *time.Time) (*time.Time, error) {
	if authorizedAt == nil {
		return nil, nil
	}
	if authorizedAt.IsZero() {
		return nil, fmt.Errorf("normalize mark at index %d: authorized at is empty", index)
	}
	normalized := yttimestamp.Normalize(*authorizedAt)
	return &normalized, nil
}

func alarmSentMarkIdentity(kind domain.OutboxKind, contentID string) string {
	return string(kind) + "\x00" + canonicalTrackingIdentity(kind, contentID)
}

func mergeAlarmSentMark(existing *AlarmSentMark, incoming AlarmSentMark, identity string, index int) error {
	if incoming.AuthorizedAt != nil {
		if err := mergeAlarmSentAuthorizedAt(existing, incoming.AuthorizedAt, identity, index); err != nil {
			return err
		}
	}
	if incoming.AlarmSentAt.Before(existing.AlarmSentAt) {
		existing.AlarmSentAt = incoming.AlarmSentAt
	}
	return nil
}

func mergeAlarmSentAuthorizedAt(existing *AlarmSentMark, incomingAuthorizedAt *time.Time, identity string, index int) error {
	if existing.AuthorizedAt == nil {
		existing.AuthorizedAt = incomingAuthorizedAt
		return nil
	}
	if !existing.AuthorizedAt.UTC().Equal(incomingAuthorizedAt.UTC()) {
		return fmt.Errorf("normalize mark at index %d: conflicting authorized_at for %s", index, identity)
	}
	return nil
}

func isCommunityShortsAlarmStateKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	default:
		return false
	}
}

func calculateLatencyResult(start *time.Time, end *time.Time) (*int64, *bool) {
	return alarmtiming.CalculateLatency(start, end)
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}
