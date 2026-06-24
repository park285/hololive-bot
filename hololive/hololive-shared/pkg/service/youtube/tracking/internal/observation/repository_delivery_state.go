package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *deliveryStateRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
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

	if err := inPgxTx(ctx, r.db, func(tx trackingDB) error {
		txRepo := NewRepositoryContext(ctx, tx)
		return txRepo.delivery.applyAlarmSentMarks(ctx, normalized)
	}); err != nil {
		return fmt.Errorf("mark alarm sent batch transaction: %w", err)
	}

	return nil
}

func (r *deliveryStateRepository) applyAlarmSentMarks(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}

	inputs, err := newBulkAlarmSentMarkInputs(marks)
	if err != nil {
		return err
	}

	updatedAt := yttimestamp.Normalize(time.Now())
	var trackingUpdated int64
	var claimedStateFinalized int64
	var authorizationMismatches int64
	var existingStateUpdated int64
	var missingStateInserted int64
	if err := r.db.QueryRow(ctx, bulkApplyAlarmSentMarksSQL, inputs.kinds, inputs.contentIDs, inputs.canonicalContentIDs, inputs.alarmSentAts, inputs.authorizedAts, updatedAt).Scan(
		&trackingUpdated,
		&claimedStateFinalized,
		&authorizationMismatches,
		&existingStateUpdated,
		&missingStateInserted,
	); err != nil {
		return fmt.Errorf("bulk mark alarm sent: %w", err)
	}
	if authorizationMismatches > 0 {
		return fmt.Errorf("bulk mark alarm sent: claim authorization mismatch count=%d", authorizationMismatches)
	}

	return nil
}

func (r *deliveryStateRepository) applyAlarmSentMark(ctx context.Context, mark AlarmSentMark, updatedAt time.Time) error {
	trackingRow, err := r.owner.FindByIdentity(ctx, mark.Kind, mark.ContentID)
	if err != nil {
		return fmt.Errorf("load tracking row: %w", err)
	}
	latencyMillis, latencyExceeded := calculateLatencyResult(nil, &mark.AlarmSentAt)
	targetContentID := mark.ContentID
	if trackingRow != nil {
		targetContentID = trackingRow.ContentID
		latencyMillis, latencyExceeded = calculateLatencyResult(trackingRow.ActualPublishedAt, &mark.AlarmSentAt)
	}

	if _, err := dbx.ExecSQL(ctx, r.db, "update tracking alarm sent row", `
		UPDATE youtube_content_alarm_tracking
		SET alarm_sent_at = ?,
		    alarm_latency_millis = ?,
		    alarm_latency_exceeded = ?,
		    delivery_status = ?,
		    updated_at = ?
		WHERE kind = ? AND content_id = ?
		  AND (alarm_sent_at IS NULL OR alarm_sent_at > ?)
	`, mark.AlarmSentAt, nullableInt64Value(latencyMillis), nullableBoolValue(latencyExceeded),
		domain.YouTubeContentAlarmDeliveryStatusSent, updatedAt, mark.Kind, targetContentID, mark.AlarmSentAt); err != nil {
		return err
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

func (r *deliveryStateRepository) applyAlarmStateSentMark(
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

	stateRow, err := r.owner.FindAlarmStateByPostID(ctx, mark.Kind, postID)
	if err != nil {
		return fmt.Errorf("load alarm state row: %w", err)
	}

	if stateRow != nil {
		return r.applyExistingAlarmStateSentMark(ctx, mark, postID, stateRow, updatedAt)
	}

	return r.applyMissingAlarmStateSentMark(ctx, mark, postID, targetContentID, trackingRow)
}

func (r *deliveryStateRepository) finalizeClaimedAlarmState(ctx context.Context, mark AlarmSentMark, postID string, updatedAt time.Time) (bool, error) {
	if mark.AuthorizedAt == nil {
		return false, nil
	}

	rowsAffected, err := dbx.ExecSQL(ctx, r.db, "finalize claimed alarm state: update row", `
		UPDATE youtube_community_shorts_alarm_states
		SET authorized_at = NULL,
		    alarm_sent_at = ?,
		    delivery_status = ?,
		    updated_at = ?
		WHERE kind = ? AND post_id = ?
		  AND authorized_at = ?
		  AND alarm_sent_at IS NULL
	`, mark.AlarmSentAt, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedAt, mark.Kind, postID, *mark.AuthorizedAt)
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (r *deliveryStateRepository) applyExistingAlarmStateSentMark(ctx context.Context, mark AlarmSentMark, postID string, stateRow *domain.YouTubeCommunityShortsAlarmState, updatedAt time.Time) error {
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

func (r *deliveryStateRepository) applyMissingAlarmStateSentMark(ctx context.Context, mark AlarmSentMark, postID, targetContentID string, trackingRow *domain.YouTubeContentAlarmTracking) error {
	if trackingRow == nil {
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: tracking row missing")
		}
		return nil
	}

	alarmSentAt := mark.AlarmSentAt
	if err := r.owner.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
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

func (r *deliveryStateRepository) updateAlarmStateSentRow(ctx context.Context, kind domain.OutboxKind, postID string, alarmSentAt, updatedAt time.Time) error {
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return err
	}

	if _, err := dbx.ExecSQL(ctx, r.db, "update alarm state sent row", `
		UPDATE youtube_community_shorts_alarm_states
		SET authorized_at = NULL,
		    alarm_sent_at = CASE
		        WHEN alarm_sent_at IS NULL OR alarm_sent_at > ? THEN ?
		        ELSE alarm_sent_at
		    END,
		    delivery_status = ?,
		    updated_at = ?
		WHERE kind = ? AND post_id = ?
		  AND (alarm_sent_at IS NULL OR alarm_sent_at > ? OR authorized_at IS NOT NULL)
	`, alarmSentAt, alarmSentAt, domain.YouTubeCommunityShortsAlarmStateStatusSent,
		updatedAt, normalizedKind, normalizedPostID, alarmSentAt); err != nil {
		return err
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
	return string(kind) + "\x00" + contentID
}

func mergeAlarmSentMark(existing *AlarmSentMark, next AlarmSentMark, identity string, index int) error {
	if next.AlarmSentAt.Before(existing.AlarmSentAt) {
		existing.AlarmSentAt = next.AlarmSentAt
	}
	if existing.AuthorizedAt == nil {
		existing.AuthorizedAt = next.AuthorizedAt
		return nil
	}
	if next.AuthorizedAt == nil {
		return nil
	}
	if !existing.AuthorizedAt.Equal(*next.AuthorizedAt) {
		return fmt.Errorf("normalize mark at index %d: conflicting authorized_at for %s", index, identity)
	}
	return nil
}

func isCommunityShortsAlarmStateKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return false
	default:
		return false
	}
}

func calculateLatencyResult(actualPublishedAt *time.Time, alarmSentAt *time.Time) (*int64, *bool) {
	return alarmtiming.CalculateLatency(actualPublishedAt, alarmSentAt)
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
