package observation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *alarmStateRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	if record == nil {
		return fmt.Errorf("upsert alarm state: record is nil")
	}
	return r.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{record})
}

func (r *alarmStateRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("try claim alarm state: db is nil")
	}

	normalizedRecord, err := normalizeAlarmStateClaim(record)
	if err != nil {
		return false, fmt.Errorf("try claim alarm state: %w", err)
	}

	return r.insertAlarmStateClaim(ctx, normalizedRecord)
}

func (r *alarmStateRepository) insertAlarmStateClaim(
	ctx context.Context,
	normalizedRecord *domain.YouTubeCommunityShortsAlarmState,
) (bool, error) {
	now := yttimestamp.Normalize(time.Now())
	var returnedAuthorizedAt time.Time
	var returnedAlarmSentAt pgtype.Timestamptz
	err := r.db.QueryRow(ctx, `
		INSERT INTO youtube_community_shorts_alarm_states
		    (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (kind, post_id) DO UPDATE
		SET content_id = EXCLUDED.content_id,
		    channel_id = EXCLUDED.channel_id,
		    actual_published_at = COALESCE(youtube_community_shorts_alarm_states.actual_published_at, EXCLUDED.actual_published_at),
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_alarm_states.detected_at
		    END,
		    authorized_at = EXCLUDED.authorized_at,
		    delivery_status = EXCLUDED.delivery_status,
		    updated_at = EXCLUDED.updated_at
		WHERE youtube_community_shorts_alarm_states.authorized_at IS NULL
		  AND youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
		RETURNING authorized_at, alarm_sent_at
	`,
		normalizedRecord.Kind,
		normalizedRecord.PostID,
		normalizedRecord.ContentID,
		normalizedRecord.ChannelID,
		normalizedRecord.ActualPublishedAt,
		normalizedRecord.DetectedAt,
		normalizeDatabaseTimestamp(*normalizedRecord.AuthorizedAt),
		nil,
		normalizedRecord.DeliveryStatus,
		now,
		now,
	).Scan(&returnedAuthorizedAt, &returnedAlarmSentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return alarmStateClaimMatches(returnedAuthorizedAt, returnedAlarmSentAt, *normalizedRecord.AuthorizedAt), nil
}

func alarmStateClaimMatches(returnedAuthorizedAt time.Time, returnedAlarmSentAt pgtype.Timestamptz, expectedAuthorizedAt time.Time) bool {
	if returnedAuthorizedAt.IsZero() {
		return false
	}
	if returnedAlarmSentAt.Valid && !returnedAlarmSentAt.Time.IsZero() {
		return false
	}
	return normalizeDatabaseTimestamp(returnedAuthorizedAt).Equal(normalizeDatabaseTimestamp(expectedAuthorizedAt))
}

func (r *alarmStateRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
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
	rowsAffected, err := dbx.ExecSQL(ctx, r.db, "release alarm state claim: update row", `
		UPDATE youtube_community_shorts_alarm_states
		SET authorized_at = NULL,
		    delivery_status = ?,
		    updated_at = ?
		WHERE kind = ? AND post_id = ?
		  AND alarm_sent_at IS NULL
		  AND authorized_at = ?
	`, domain.YouTubeCommunityShortsAlarmStateStatusDetected, updatedAt, normalizedKind, normalizedPostID, normalizeDatabaseTimestamp(authorizedAt))
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}
