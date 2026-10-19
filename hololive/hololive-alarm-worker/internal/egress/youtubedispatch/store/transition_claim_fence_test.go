package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBeginSendingRejectsStaleClaimVersion(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-claim-fence-begin", "room-claim-fence-begin")

	first := newTestTransitionStore(t, pool)
	firstClaim := claimAndPrepare(t, pool, first)

	expireTransitionClaimLease(t, pool, deliveryID)

	second := newTestTransitionStore(t, pool)
	secondClaim := claimAndPrepare(t, pool, second)
	require.Equal(t, firstClaim.rows[0].RowVersion+1, secondClaim.rows[0].RowVersion)

	operation, result, err := first.BeginSending(ctx, firstClaim.rows, firstClaim.outboxes)
	requireClaimFenceMiss(t, err, deliveryID, sendfence.ReasonVersionChanged)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.False(t, operation.Valid())
	assertTransitionClaimRow(t, pool, deliveryID, domain.OutboxStatusPending, secondClaim.rows[0].RowVersion)

	operation, result, err = second.BeginSending(ctx, secondClaim.rows, secondClaim.outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.True(t, operation.Valid())
	assertTransitionClaimRow(t, pool, deliveryID, DeliveryStatusSending, secondClaim.rows[0].RowVersion+1)
}

func TestApplyPreparedFailureRejectsStaleClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-claim-fence-failure", "room-claim-fence-failure")

	first := newTestTransitionStore(t, pool)
	firstClaim := claimAndPrepare(t, pool, first)

	expireTransitionClaimLease(t, pool, deliveryID)

	second := newTestTransitionStore(t, pool)
	secondClaim := claimAndPrepare(t, pool, second)

	result, err := first.ApplyPreparedFailure(
		ctx, firstClaim.rows, firstClaim.outboxes,
		lifecycle.FailurePermanent, lifecycle.Reason("message_missing"), 0, DeliveryModePerRoom,
	)
	requireClaimFenceMiss(t, err, deliveryID, sendfence.ReasonVersionChanged)
	require.Equal(t, ApplyConflict, result.Outcome)
	assertTransitionClaimRow(t, pool, deliveryID, domain.OutboxStatusPending, secondClaim.rows[0].RowVersion)
}

func TestPrepareClaimedRejectsForeignClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-claim-fence-prepare", "room-claim-fence-prepare")

	first := newTestTransitionStore(t, pool)
	firstClaimed, err := first.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, firstClaimed, 2)

	expireTransitionClaimLease(t, pool, ownerID, followerID)

	second := newOwnedTestTransitionStore(t, pool, "youtube-dispatcher:other")
	secondClaimed, err := second.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, secondClaimed, 2)

	for i := range secondClaimed {
		require.Equal(t, new("youtube-dispatcher:other"), secondClaimed[i].LockedBy)
	}

	_, err = first.PrepareClaimed(ctx, firstClaimed, loadTransitionTestOutboxes(t, pool, firstClaimed))

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "error %v is not a send fence miss", err)
	require.Equal(t, 0, miss.Applied)
	require.Equal(t, []sendfence.RowMiss{
		{ID: ownerID, Reason: sendfence.ReasonOwnerChanged, Status: string(domain.OutboxStatusPending)},
		{ID: followerID, Reason: sendfence.ReasonOwnerChanged, Status: string(domain.OutboxStatusPending)},
	}, miss.Rows)
	assertTransitionClaimRow(t, pool, ownerID, domain.OutboxStatusPending, 2)
	assertTransitionClaimRow(t, pool, followerID, domain.OutboxStatusPending, 2)
}

func TestBeginSendingRejectsExpiredClaimLease(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-claim-fence-lease", "room-claim-fence-lease")

	transition := newTestTransitionStore(t, pool)
	claim := claimAndPrepare(t, pool, transition)

	expireTransitionClaimLease(t, pool, deliveryID)

	operation, result, err := transition.BeginSending(ctx, claim.rows, claim.outboxes)
	requireClaimFenceMiss(t, err, deliveryID, sendfence.ReasonLeaseExpired)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.False(t, operation.Valid())
	assertTransitionClaimRow(t, pool, deliveryID, domain.OutboxStatusPending, claim.rows[0].RowVersion)
}

// 발송 뒤 정산은 외부 결과를 lease 만료로 되돌리지 않으므로 Send fence만 보고 lease를 보지 않는다.
func TestSettlementAfterSendingIgnoresExpiredLease(t *testing.T) {
	tests := []struct {
		name    string
		settle  func(context.Context, *TransitionStore, StartedOperation) (ApplyResult, error)
		status  domain.OutboxStatus
		attempt int
	}{
		{
			name: "complete sent",
			settle: func(ctx context.Context, transition *TransitionStore, operation StartedOperation) (ApplyResult, error) {
				return transition.CompleteSent(ctx, operation, nil, DeliveryModePerRoom)
			},
			status: domain.OutboxStatusSent,
		},
		{
			name: "retryable provider failure",
			settle: func(ctx context.Context, transition *TransitionStore, operation StartedOperation) (ApplyResult, error) {
				return transition.ApplyStartedFailure(
					ctx, operation, lifecycle.FailureRetryable, lifecycle.Reason("provider_transport"), 0, DeliveryModePerRoom,
				)
			},
			status: domain.OutboxStatusPending, attempt: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			deliveryID := seedTransitionSingleDelivery(t, pool, "video-claim-fence-settle", "room-claim-fence-settle")

			transition := newTestTransitionStore(t, pool)
			claim := claimAndPrepare(t, pool, transition)

			operation, begin, err := transition.BeginSending(t.Context(), claim.rows, claim.outboxes)
			require.NoError(t, err)
			require.Equal(t, ApplyApplied, begin.Outcome)

			expireTransitionClaimLease(t, pool, deliveryID)

			result, err := tc.settle(t.Context(), transition, operation)
			require.NoError(t, err)
			require.Equal(t, ApplyApplied, result.Outcome)

			var (
				status   domain.OutboxStatus
				attempt  int
				lockedBy *string
			)

			require.NoError(t, pool.QueryRow(t.Context(), `
				SELECT status, attempt_count, locked_by FROM youtube_notification_delivery WHERE id = $1
			`, deliveryID).Scan(&status, &attempt, &lockedBy))
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.attempt, attempt)
			require.Nil(t, lockedBy)
		})
	}
}

type transitionTestClaim struct {
	rows     []domain.YouTubeNotificationDelivery
	outboxes map[int64]domain.YouTubeNotificationOutbox
}

func claimAndPrepare(t *testing.T, pool *pgxpool.Pool, transition *TransitionStore) transitionTestClaim {
	t.Helper()

	claimed, err := transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.NoError(t, err)
	require.Len(t, prepared.ActiveRows, 1)

	return transitionTestClaim{rows: prepared.ActiveRows, outboxes: outboxes}
}

// expireTransitionClaimLease는 버전을 바꾸지 않고 claim 시각만 lease 밖으로 옮겨 정지했던 소유자를 재현한다.
func expireTransitionClaimLease(t *testing.T, pool *pgxpool.Pool, ids ...int64) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery
		SET locked_at = locked_at - interval '2 minutes'
		WHERE id = ANY($1::bigint[])
	`, ids)
	require.NoError(t, err)
}

func requireClaimFenceMiss(t *testing.T, err error, deliveryID int64, reason sendfence.Reason) {
	t.Helper()

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "error %v is not a send fence miss", err)
	require.Equal(t, 0, miss.Applied)
	require.Equal(t, []sendfence.RowMiss{{ID: deliveryID, Reason: reason, Status: string(domain.OutboxStatusPending)}}, miss.Rows)
}

func assertTransitionClaimRow(t *testing.T, pool *pgxpool.Pool, deliveryID int64, status domain.OutboxStatus, rowVersion int64) {
	t.Helper()

	var (
		actualStatus  domain.OutboxStatus
		actualVersion int64
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, row_version FROM youtube_notification_delivery WHERE id = $1
	`, deliveryID).Scan(&actualStatus, &actualVersion))
	require.Equal(t, status, actualStatus)
	require.Equal(t, rowVersion, actualVersion)
}

func seedTransitionSingleDelivery(t *testing.T, pool *pgxpool.Pool, contentID, roomID string) int64 {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	var outboxID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox (
			kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
		) VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $5)
		RETURNING id
	`, domain.OutboxKindNewVideo, "channel-claim-fence", contentID, domain.OutboxStatusPending, createdAt).Scan(&outboxID))

	var deliveryID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_delivery (
			outbox_id, room_id, status, attempt_count, next_attempt_at, created_at
		) VALUES ($1, $2, $3, 0, $4, $4)
		RETURNING id
	`, outboxID, roomID, domain.OutboxStatusPending, createdAt).Scan(&deliveryID))

	return deliveryID
}
