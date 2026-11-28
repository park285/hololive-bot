package durability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	var firstCheck, accepted int64

	require.NoError(t, repo.store.QueryRow(ctx, `SELECT handoff_next_check_at, accepted_at
		FROM bot_reply_outbox WHERE id = ?1`, claim.ID).Scan(&firstCheck, &accepted))
	require.Equal(t, time.Millisecond.Milliseconds(), firstCheck-accepted, "first handoff check delay")

	return claim.ID
}

func advanceHandoffChecks(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository) {
	t.Helper()

	_, err := repo.store.Exec(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1,
		    accepted_at = ?1 - (handoff_next_check_at - accepted_at),
		    handoff_next_check_at = ?1
		WHERE status = 'accepted'`, time.Now().UnixMilli())
	require.NoError(t, err)
}

func claimOneHandoffDue(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository) ReplyHandoffClaim {
	t.Helper()
	advanceHandoffChecks(ctx, t, repo)

	claims, err := repo.ClaimHandoffDue(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)

	return claims[0]
}

func completeReplyHandoff(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, claim *ReplyOutboxClaim) {
	t.Helper()

	accepted, err := repo.SettleAccepted(ctx, claim.ID, testClaimToken, "iris-"+claim.ClientRequestID, time.Millisecond)
	require.NoError(t, err)
	require.True(t, accepted)

	completed, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: claimOneHandoffDue(ctx, t, repo), Status: ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	require.True(t, completed)
}

func TestReplyHandoffClaimsAreDisjointUnderConcurrencyAndSoftClaimed(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	first := newReplyOutboxEntry("message:handoff-first", 0, `{"kind":"text","message":"a"}`)
	second := newReplyOutboxEntry("message:handoff-second", 0, `{"kind":"text","message":"b"}`)

	second.RoomID = "room-2"

	firstID := acceptReply(ctx, t, repo, first, "iris-first")
	secondID := acceptReply(ctx, t, repo, second, "iris-second")

	advanceHandoffChecks(ctx, t, repo)

	start := make(chan struct{})
	results := make(chan []ReplyHandoffClaim, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			<-start

			claims, claimErr := repo.ClaimHandoffDue(ctx, 1, time.Minute)
			results <- claims

			errs <- claimErr
		})
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	seen := map[int64]ReplyHandoffClaim{}

	for claims := range results {
		require.Len(t, claims, 1)
		require.NotContains(t, seen, claims[0].ID, "동시 확인은 같은 행을 두 번 고르지 않는다")

		seen[claims[0].ID] = claims[0]
	}

	require.Contains(t, seen, firstID)
	require.Contains(t, seen, secondID)
	assert.Equal(t, "iris-second", seen[secondID].IrisRequestID)
	assert.Equal(t, int32(1), seen[secondID].Attempts)
	assert.False(t, seen[secondID].ReplayHorizonExpired)
	assert.GreaterOrEqual(t, seen[secondID].Elapsed, time.Millisecond)

	again, err := repo.ClaimHandoffDue(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "soft claim한 행은 미룬 시각 전까지 다시 고르지 않는다")
}

func TestReplyHandoffTransitionsAreFencedByClaimTokenAndRequestID(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

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

	mismatched := current

	mismatched.IrisRequestID = "iris-other"

	applied, err = repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: mismatched, Status: ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	assert.False(t, applied, "다른 Iris requestId의 관측은 이 행을 바꾸지 않는다")

	_, err = repo.SettleHandoff(ctx, ReplyHandoffSettlement{Claim: current, Status: ReplyOutboxOutcomeUnknown})
	require.ErrorIs(t, err, ErrInvalidArgument, "accepted에서 같은 ID 재확인 상태로는 가지 않는다")

	status, payload, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxAccepted, status)
	assert.NotNil(t, payload)
}

func TestReplyHandoffSettlementsShapeTheRow(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
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
			truncateDurabilityTables(ctx, t, store)

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

			require.NoError(t, store.QueryRow(ctx, `SELECT status, last_error, payload IS NOT NULL, handoff_next_check_at IS NULL
				FROM bot_reply_outbox WHERE id = ?1`, claim.ID).Scan(&status, &lastError, &payloadKept, &checkCleared))
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

func TestReplyHandoffFailedReturnsTheStoredReplyToTheClaimQueue(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:handoff-failed", 0, `{"kind":"text","message":"stored"}`)
	acceptReply(ctx, t, repo, entry, "iris-failed")

	claim := claimOneHandoffDue(ctx, t, repo)

	applied, err := repo.SettleHandoff(ctx, ReplyHandoffSettlement{
		Claim: claim, Status: ReplyOutboxRetryablePreDispatch, LastError: "handoff_failed", RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)
	advanceReplyRetry(ctx, t, store, claim.ID)

	resend, err := repo.Claim(ctx, "token-resend", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, resend)
	assert.Equal(t, claim.ID, resend.ID)
	assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID)
	assert.JSONEq(t, `{"kind":"text","message":"stored"}`, string(resend.Payload))
	assert.Equal(t, int32(2), resend.Attempts)
}

func TestReplyHandoffRescheduleNeverPassesTheHorizon(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:handoff-horizon", 0, `{"kind":"text","message":"a"}`)
	id := acceptReply(ctx, t, repo, entry, "iris-horizon")
	claim := claimOneHandoffDue(ctx, t, repo)

	delay, applied, err := repo.RescheduleHandoff(ctx, claim, time.Hour, 2*time.Second)
	require.NoError(t, err)
	require.True(t, applied)
	assert.LessOrEqual(t, delay, 2*time.Second)

	var atHorizon bool

	require.NoError(t, store.QueryRow(ctx, `SELECT handoff_next_check_at = accepted_at + 2000
		FROM bot_reply_outbox WHERE id = ?1`, id).Scan(&atHorizon))
	assert.True(t, atHorizon, "지평 시각의 확인이 마지막 확인이다")

	status, _, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxAccepted, status, "재예약은 상태를 바꾸지 않는다")
}

func TestReplyHandoffAcceptedPredecessorHoldsTheRoomUntilConfirmed(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

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
