package store

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestApplyPreparedFailureIsNotSplitByConcurrentFollowerClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-prepared-failure-race", "room-prepared-failure-race")
	transition := newTestTransitionStore(t, pool)
	prepared, outboxes := claimTransitionTestActiveRows(t, transition, pool)
	require.Equal(t, ownerID, prepared[0].ID)

	interleaved := false

	transition.afterCommit = func(string) error {
		if interleaved {
			return nil
		}

		interleaved = true

		_, err := pool.Exec(ctx, `
			UPDATE youtube_notification_delivery
			SET locked_by = 'youtube-dispatcher:other', locked_at = now(), row_version = row_version + 1
			WHERE id = $1 AND status = $2
		`, followerID, domain.OutboxStatusPending)

		return err
	}

	result, err := transition.ApplyPreparedFailure(
		ctx, prepared, outboxes, lifecycle.FailurePermanent, "message_missing", 0,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.True(t, interleaved)

	for _, id := range []int64{ownerID, followerID} {
		var (
			status   domain.OutboxStatus
			lockedBy *string
		)

		require.NoError(t, pool.QueryRow(ctx, `
			SELECT status, locked_by FROM youtube_notification_delivery WHERE id = $1
		`, id).Scan(&status, &lockedBy))
		require.Equal(t, domain.OutboxStatusFailed, status, "delivery %d", id)
		require.Nil(t, lockedBy, "delivery %d", id)
	}
}

func TestApplyPreparedFailureConfirmsLostCommitResponse(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	ownerID, _ := seedTransitionShortLogicalGroup(t, pool, "short-prepared-failure-lost", "room-prepared-failure-lost")
	transition := newTestTransitionStore(t, pool)
	prepared, outboxes := claimTransitionTestActiveRows(t, transition, pool)

	transition.afterCommit = func(operation string) error {
		if operation == "apply prepared failure" {
			return errors.New("prepared failure commit response lost")
		}

		return nil
	}

	result, err := transition.ApplyPreparedFailure(
		ctx, prepared, outboxes, lifecycle.FailureRetryable, "message_missing", 0,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, CommitConfirmedPost, result.CommitAdjudication)
	require.Equal(t, []lifecycle.RuleID{lifecycle.RuleRetryScheduled}, result.Rules)

	var attemptCount int

	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_count FROM youtube_notification_delivery WHERE id = $1`, ownerID).Scan(&attemptCount))
	require.Equal(t, 1, attemptCount)

	var spooled int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_notification_delivery_telemetry`).Scan(&spooled))
	require.Zero(t, spooled)
}

func seedTransitionShortLogicalGroup(t *testing.T, pool *pgxpool.Pool, videoID, roomID string) (int64, int64) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	payload := transitionShortPayload(videoID)

	var ids [2]int64

	for index, contentID := range []string{videoID, " " + videoID + " "} {
		var outboxID int64

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_outbox (
				kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $7)
			RETURNING id
		`, domain.OutboxKindNewShort, "channel-telemetry", contentID, payload, domain.OutboxStatusPending,
			createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&outboxID))

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_delivery (
				outbox_id, room_id, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, 0, $4, $5)
			RETURNING id
		`, outboxID, roomID, domain.OutboxStatusPending, createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&ids[index]))
	}

	return ids[0], ids[1]
}

func transitionShortPayload(videoID string) string {
	return `{"canonical_post_id":"short:` + videoID + `","video_id":"` + videoID + `","title":"telemetry"}`
}

func claimTransitionTestActiveRows(
	t *testing.T,
	transition *TransitionStore,
	pool *pgxpool.Pool,
) ([]domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) {
	t.Helper()

	claimed, err := transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, 1)

	return prepared.ActiveRows, outboxes
}
