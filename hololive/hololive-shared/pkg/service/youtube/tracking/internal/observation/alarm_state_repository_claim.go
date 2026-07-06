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
	err := r.db.QueryRow(ctx, mustSQL("alarm_state_repository_claim_0044_01.sql"),
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
	rowsAffected, err := dbx.ExecSQL(ctx, r.db, "release alarm state claim: update row", mustSQL("alarm_state_repository_claim_0109_02.sql"), domain.YouTubeCommunityShortsAlarmStateStatusDetected, updatedAt, normalizedKind, normalizedPostID, normalizeDatabaseTimestamp(authorizedAt))
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}
