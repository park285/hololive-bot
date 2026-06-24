package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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

const bulkApplyAlarmSentMarksSQL = `
WITH input AS (
	SELECT *
	FROM unnest(
		$1::text[],
		$2::text[],
		$3::text[],
		$4::timestamptz[],
		$5::timestamptz[]
	) AS t(kind, content_id, canonical_content_id, alarm_sent_at, authorized_at)
), deduped_input AS (
	SELECT DISTINCT ON (kind, canonical_content_id)
		kind,
		content_id,
		canonical_content_id,
		alarm_sent_at,
		authorized_at
	FROM input
	WHERE kind <> ''
	  AND canonical_content_id <> ''
	  AND alarm_sent_at IS NOT NULL
	ORDER BY kind, canonical_content_id, alarm_sent_at ASC
), tracking_updated AS (
	UPDATE youtube_content_alarm_tracking AS t
	SET alarm_sent_at = i.alarm_sent_at,
	    alarm_latency_millis = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT)
	    END,
	    alarm_latency_exceeded = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT) > 120000 THEN TRUE
	        ELSE FALSE
	    END,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE t.kind = i.kind
	  AND t.canonical_content_id = i.canonical_content_id
	  AND (t.alarm_sent_at IS NULL OR t.alarm_sent_at > i.alarm_sent_at)
	RETURNING
		t.kind,
		t.canonical_content_id AS post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		t.alarm_sent_at
), claimed_state_finalized AS (
	UPDATE youtube_community_shorts_alarm_states AS s
	SET authorized_at = NULL,
	    alarm_sent_at = i.alarm_sent_at,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND s.post_id = i.canonical_content_id
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.authorized_at = i.authorized_at
	  AND s.alarm_sent_at IS NULL
	RETURNING s.kind, s.post_id
), authorization_mismatches AS (
	SELECT s.kind, s.post_id
	FROM deduped_input AS i
	JOIN youtube_community_shorts_alarm_states AS s
	  ON s.kind = i.kind
	 AND s.post_id = i.canonical_content_id
	WHERE i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.alarm_sent_at IS NULL
	  AND NOT EXISTS (
		SELECT 1
		FROM claimed_state_finalized AS f
		WHERE f.kind = s.kind
		  AND f.post_id = s.post_id
	  )
), existing_state_updated AS (
	UPDATE youtube_community_shorts_alarm_states AS s
	SET authorized_at = NULL,
	    alarm_sent_at = CASE
	        WHEN s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at THEN i.alarm_sent_at
	        ELSE s.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND s.post_id = i.canonical_content_id
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND NOT EXISTS (
		SELECT 1
		FROM claimed_state_finalized AS f
		WHERE f.kind = s.kind
		  AND f.post_id = s.post_id
	  )
	  AND NOT EXISTS (
		SELECT 1
		FROM authorization_mismatches AS m
		WHERE m.kind = s.kind
		  AND m.post_id = s.post_id
	  )
	  AND (s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at OR s.authorized_at IS NOT NULL)
	RETURNING s.kind, s.post_id
), missing_state_inserted AS (
	INSERT INTO youtube_community_shorts_alarm_states (
		kind,
		post_id,
		content_id,
		channel_id,
		actual_published_at,
		detected_at,
		authorized_at,
		alarm_sent_at,
		delivery_status,
		created_at,
		updated_at
	)
	SELECT
		t.kind,
		t.post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		NULL,
		t.alarm_sent_at,
		'SENT',
		$6,
		$6
	FROM tracking_updated AS t
	WHERE t.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND NOT EXISTS (
		SELECT 1
		FROM youtube_community_shorts_alarm_states AS s
		WHERE s.kind = t.kind
		  AND s.post_id = t.post_id
	  )
	ON CONFLICT (kind, post_id) DO UPDATE
	SET alarm_sent_at = CASE
	        WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
	          OR youtube_community_shorts_alarm_states.alarm_sent_at > EXCLUDED.alarm_sent_at
	        THEN EXCLUDED.alarm_sent_at
	        ELSE youtube_community_shorts_alarm_states.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    authorized_at = NULL,
	    updated_at = $6
	RETURNING kind, post_id
)
SELECT
	(SELECT COUNT(*) FROM tracking_updated) AS tracking_updated_count,
	(SELECT COUNT(*) FROM claimed_state_finalized) AS claimed_state_finalized_count,
	(SELECT COUNT(*) FROM authorization_mismatches) AS authorization_mismatch_count,
	(SELECT COUNT(*) FROM existing_state_updated) AS existing_state_updated_count,
	(SELECT COUNT(*) FROM missing_state_inserted) AS missing_state_inserted_count
`

func (r *deliveryStateRepository) applyAlarmSentMarks(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}

	kinds := make([]string, 0, len(marks))
	contentIDs := make([]string, 0, len(marks))
	canonicalContentIDs := make([]string, 0, len(marks))
	alarmSentAts := make([]time.Time, 0, len(marks))
	authorizedAts := make([]pgtype.Timestamptz, 0, len(marks))

	for i, mark := range marks {
		if mark.AlarmSentAt.IsZero() {
			return fmt.Errorf("bulk mark alarm sent: alarm sent at is empty at index %d", i)
		}
		canonicalContentID := canonicalTrackingIdentity(mark.Kind, mark.ContentID)
		if strings.TrimSpace(canonicalContentID) == "" {
			return fmt.Errorf("bulk mark alarm sent: canonical content id is empty at index %d", i)
		}

		kinds = append(kinds, string(mark.Kind))
		contentIDs = append(contentIDs, mark.ContentID)
		canonicalContentIDs = append(canonicalContentIDs, canonicalContentID)
		alarmSentAts = append(alarmSentAts, mark.AlarmSentAt)
		if mark.AuthorizedAt == nil {
			authorizedAts = append(authorizedAts, pgtype.Timestamptz{})
			continue
		}
		authorizedAts = append(authorizedAts, pgtype.Timestamptz{Time: *mark.AuthorizedAt, Valid: true})
	}

	updatedAt := yttimestamp.Normalize(time.Now())
	var trackingUpdated int64
	var claimedStateFinalized int64
	var authorizationMismatches int64
	var existingStateUpdated int64
	var missingStateInserted int64
	if err := r.db.QueryRow(ctx, bulkApplyAlarmSentMarksSQL, kinds, contentIDs, canonicalContentIDs, alarmSentAts, authorizedAts, updatedAt).Scan(
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

func calculateLatencyResult(actualPublishedAt *time.Time, alarmSentAt *time.Time) (*int64, *bool) {
	result := alarmtiming.CalculateLatencyResult(actualPublishedAt, alarmSentAt)
	return result.LatencyMillis, result.Exceeded
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
