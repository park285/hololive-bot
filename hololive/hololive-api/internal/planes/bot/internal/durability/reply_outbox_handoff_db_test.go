package durability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// acceptReply는 행을 stage·claim한 뒤 Iris admission 정산까지 진행해 1ms 뒤 확인 예정인 lease 없는 accepted로 만든다.
func acceptReply(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, entry *ReplyOutboxEntry, irisRequestID string) int64 {
	t.Helper()

	_, err := repo.Insert(ctx, entry)
	require.NoError(t, err)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, entry.ClientRequestID, claim.ClientRequestID)

	applied, err := repo.SettleAccepted(ctx, claim.ID, testClaimToken, irisRequestID, time.Millisecond)
	require.NoError(t, err)
	require.True(t, applied)

	return claim.ID
}

// claimOneHandoffDue는 첫 확인 시각이 DB 시계로 지나기를 기다려 handoff 확인 하나를 claim한다.
func claimOneHandoffDue(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository) ReplyHandoffClaim {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		claims, err := repo.ClaimHandoffDue(ctx, 10, time.Minute)
		require.NoError(t, err)

		if len(claims) > 0 || time.Now().After(deadline) {
			require.Len(t, claims, 1)

			return claims[0]
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// completeReplyHandoff는 발송 claim을 admission 정산한 뒤 reconciler 경로로 handoff 완료까지 진행한다.
func completeReplyHandoff(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, claim *ReplyOutboxClaim) {
	t.Helper()

	accepted, err := repo.SettleAccepted(ctx, claim.ID, testClaimToken, "iris-"+claim.ClientRequestID, time.Millisecond)
	require.NoError(t, err)
	require.True(t, accepted)

	completed, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: claimOneHandoffDue(ctx, t, repo), Status: ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	require.True(t, completed)
}

func TestReplyHandoffClaimSkipsLockedRowsAndSoftClaimsWhatItReturns(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)

	first := newReplyOutboxEntry("message:handoff-first", 0, `{"kind":"text","message":"a"}`)
	second := newReplyOutboxEntry("message:handoff-second", 0, `{"kind":"text","message":"b"}`)

	second.RoomID = "room-2"

	firstID := acceptReply(ctx, t, repo, first, "iris-first")
	secondID := acceptReply(ctx, t, repo, second, "iris-second")

	time.Sleep(10 * time.Millisecond)

	locker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, rollbackInboxTx(ctx, locker)) }()

	_, err = locker.Exec(ctx, `SELECT id FROM bot_reply_outbox WHERE id = $1 FOR UPDATE`, firstID)
	require.NoError(t, err)

	claims, err := repo.ClaimHandoffDue(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1, "다른 확인이 잠근 행은 건너뛴다")
	assert.Equal(t, secondID, claims[0].ID)
	assert.Equal(t, "iris-second", claims[0].IrisRequestID)
	assert.Equal(t, int32(1), claims[0].Attempts)
	assert.False(t, claims[0].ReplayHorizonExpired)
	assert.GreaterOrEqual(t, claims[0].Elapsed, time.Millisecond)

	require.NoError(t, locker.Commit(ctx))

	claims, err = repo.ClaimHandoffDue(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1, "soft claim한 행은 미룬 시각 전까지 다시 고르지 않는다")
	assert.Equal(t, firstID, claims[0].ID)
}

func TestReplyHandoffTransitionsAreFencedByClaimTokenAndRequestID(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)

	entry := newReplyOutboxEntry("message:handoff-fence", 0, `{"kind":"text","message":"a"}`)
	acceptReply(ctx, t, repo, entry, "iris-fence")

	stale := claimOneHandoffDue(ctx, t, repo)

	_, applied, err := repo.RescheduleHandoff(ctx, stale, time.Millisecond, time.Hour)
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: stale, Status: ReplyOutboxDead, LastError: "handoff_failed"})
	require.NoError(t, err)
	assert.False(t, applied, "재예약으로 바뀐 토큰의 관측은 전이하지 않는다")

	current := claimOneHandoffDue(ctx, t, repo)

	mismatched := ReplyHandoffClaim{ID: current.ID, IrisRequestID: "iris-other", CheckToken: current.CheckToken}

	applied, err = repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: mismatched, Status: ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	assert.False(t, applied, "다른 Iris requestId의 관측은 이 행을 바꾸지 않는다")

	_, err = repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: current, Status: ReplyOutboxOutcomeUnknown})
	require.ErrorIs(t, err, ErrInvalidArgument, "accepted에서 같은 ID 재확인 상태로는 가지 않는다")

	status, payload, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxAccepted, status)
	assert.NotNil(t, payload)
}

func TestReplyHandoffSettlementsShapeTheRow(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()

	cases := []struct {
		status, lastError string
		wantPayload       bool
	}{
		{status: ReplyOutboxHandoffCompleted},
		{status: ReplyOutboxDead, lastError: "handoff_failed"},
		{status: ReplyOutboxManualReview, lastError: "handoff_outcome_unknown", wantPayload: true},
		{status: ReplyOutboxRetryablePreDispatch, lastError: "handoff_failed", wantPayload: true},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			truncateDurabilityTables(ctx, t, pool)

			entry := newReplyOutboxEntry("message:handoff-"+tc.status, 0, `{"kind":"text","message":"stored"}`)
			acceptReply(ctx, t, repo, entry, "iris-"+tc.status)

			claim := claimOneHandoffDue(ctx, t, repo)

			applied, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{
				Claim: claim, Status: tc.status, LastError: tc.lastError, RetryAfter: time.Millisecond,
			})
			require.NoError(t, err)
			require.True(t, applied)

			var (
				status, lastError string
				payloadKept       bool
				checkCleared      bool
			)

			require.NoError(t, pool.QueryRow(ctx, `SELECT status, last_error, payload IS NOT NULL, handoff_next_check_at IS NULL
				FROM bot_reply_outbox WHERE id = $1`, claim.ID).Scan(&status, &lastError, &payloadKept, &checkCleared))
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.lastError, lastError)
			assert.Equal(t, tc.wantPayload, payloadKept)
			assert.True(t, checkCleared)

			again, err := repo.ClaimHandoffDue(ctx, 10, time.Minute)
			require.NoError(t, err)
			assert.Empty(t, again, "확인을 끝낸 행은 reconciler 대상이 아니다")
		})
	}
}

// failed 재발송은 같은 행을 재발송 큐로 되돌리고, 다음 발송 claim이 저장 payload와 base clientRequestId로
// 시작하게 한다. 세대는 그 claim의 reissue ladder가 409 FAILED를 보고 올린다.
func TestReplyHandoffFailedReturnsTheStoredReplyToTheClaimQueue(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)

	entry := newReplyOutboxEntry("message:handoff-failed", 0, `{"kind":"text","message":"stored"}`)
	acceptReply(ctx, t, repo, entry, "iris-failed")

	claim := claimOneHandoffDue(ctx, t, repo)

	applied, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{
		Claim: claim, Status: ReplyOutboxRetryablePreDispatch, LastError: "handoff_failed", RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)
	time.Sleep(5 * time.Millisecond)

	resend, err := repo.Claim(ctx, "token-resend", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, resend)
	assert.Equal(t, claim.ID, resend.ID)
	assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID)
	assert.JSONEq(t, `{"kind":"text","message":"stored"}`, string(resend.Payload))
	assert.Equal(t, int32(2), resend.Attempts)
}

func TestReplyHandoffRescheduleNeverPassesTheHorizon(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)

	entry := newReplyOutboxEntry("message:handoff-horizon", 0, `{"kind":"text","message":"a"}`)
	id := acceptReply(ctx, t, repo, entry, "iris-horizon")
	claim := claimOneHandoffDue(ctx, t, repo)

	delay, applied, err := repo.RescheduleHandoff(ctx, claim, time.Hour, 2*time.Second)
	require.NoError(t, err)
	require.True(t, applied)
	assert.LessOrEqual(t, delay, 2*time.Second)

	var untilHorizonMS float64

	require.NoError(t, pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM handoff_next_check_at - accepted_at) * 1000
		FROM bot_reply_outbox WHERE id = $1`, id).Scan(&untilHorizonMS))
	assert.InDelta(t, 2000, untilHorizonMS, 1, "지평 시각의 확인이 마지막 확인이다")

	status, _, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxAccepted, status, "재예약은 상태를 바꾸지 않는다")
}

// 같은 방의 accepted 선행 행은 handoff 확인이 끝날 때까지 후행 응답의 admission을 보류한다.
func TestReplyHandoffAcceptedPredecessorHoldsTheRoomUntilConfirmed(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)

	head := newReplyOutboxEntry("message:room-head", 0, `{"kind":"text","message":"first"}`)
	acceptReply(ctx, t, repo, head, "iris-head")

	follower := newReplyOutboxEntry("message:room-follower", 0, `{"kind":"text","message":"second"}`)
	_, err := repo.Insert(ctx, follower)
	require.NoError(t, err)

	blocked, err := repo.Claim(ctx, "token-follower", durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, blocked, "accepted 선행 행이 handoff 확인 전이면 후행 응답을 보내지 않는다")

	claim := claimOneHandoffDue(ctx, t, repo)
	applied, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: claim, Status: ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	require.True(t, applied)

	next, err := repo.Claim(ctx, "token-follower", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, follower.ClientRequestID, next.ClientRequestID)
}
