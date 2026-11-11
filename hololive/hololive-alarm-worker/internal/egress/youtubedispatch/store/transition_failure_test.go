package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// ApplyPreparedFailure는 논리 그룹 재조회와 실패 전이를 한 트랜잭션에서 한다. 첫 COMMIT 경계에 다른 worker가 follower를
// claim해도 그 변경은 실패 전이 뒤에 오므로 그룹 전원이 함께 실패하고 follower에는 다른 소유자가 남지 않는다.
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
		ctx, prepared, outboxes, lifecycle.FailurePermanent, "message_missing", 0, DeliveryModePerRoom,
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

// 재조회와 전이를 묶은 트랜잭션의 COMMIT 응답을 잃으면 전이 대상 행의 사후 상태로 적용을 확정하고 다시 쓰지 않는다.
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
		ctx, prepared, outboxes, lifecycle.FailureRetryable, "message_missing", 0, DeliveryModePerRoom,
	)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, CommitConfirmedPost, result.CommitAdjudication)
	require.Equal(t, []lifecycle.RuleID{lifecycle.RuleRetryScheduled}, result.Rules)

	telemetry := loadTransitionTelemetryRows(t, pool)
	require.Len(t, telemetry, 1)
	require.Equal(t, ownerID, telemetry[0].DeliveryID)
}
