package durability

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyOutboxDivergentReplayDoesNotBlockDelivery(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	stored := newReplyOutboxEntry("message:m-replay", 0, `{"body":"error notice"}`)
	outcome, err := repo.Insert(ctx, stored)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	recomputed := *stored

	recomputed.Payload = []byte(`{"body":"the real answer"}`)
	outcome, err = repo.Insert(ctx, &recomputed)
	require.NoError(t, err, "payload 불일치는 전달을 막는 실패가 아니라 관측 신호여야 한다")
	assert.Equal(t, ReplyOutboxPayloadDiverged, outcome)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim, "재계산 바이트가 달라도 저장본은 계속 발송 가능해야 한다")
	assert.JSONEq(t, `{"body":"error notice"}`, string(claim.Payload))
	assert.Equal(t, stored.ClientRequestID, claim.ClientRequestID)
}

func TestReplyOutboxOutcomeUnknownIsResendable(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:m-unknown", 0, `{"body":"answer"}`)
	claim := claimOne(ctx, t, repo, entry)

	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID:         claim.ID,
		ClaimToken: testClaimToken,
		Status:     ReplyOutboxOutcomeUnknown,
		LastError:  "network reset before the outcome was known",
		RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)
	advanceReplyRetry(ctx, t, store, claim.ID)

	resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, resend, "outcome_unknown이 재발송 불가면 사용자는 아무것도 받지 못한다")
	assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID,
		"같은 clientRequestId여야 Iris admission dedup이 중복을 흡수한다")
	assert.JSONEq(t, `{"body":"answer"}`, string(resend.Payload))
}

func TestReplyOutboxConsecutiveOutcomeUnknownBacksOffThenRequiresManualReview(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:m-consecutive-unknown", 0, `{"body":"answer"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	previousDelay := time.Duration(0)

	for attempt := int32(1); attempt <= 5; attempt++ {
		claim, err := repo.Claim(ctx, fmt.Sprintf("token-%d", attempt), durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
		assert.Equal(t, attempt, claim.Attempts)

		status := ReplyOutboxOutcomeUnknown

		if attempt == 5 {
			status = ReplyOutboxManualReview
		}

		retryAfter := time.Minute * time.Duration(1<<(attempt-1))
		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID: claim.ID, ClaimToken: fmt.Sprintf("token-%d", attempt), Status: status,
			LastError: "outcome remained unknown", RetryAfter: retryAfter,
		})
		require.NoError(t, err)
		require.True(t, applied)

		if attempt < 5 {
			var delayMS int64

			require.NoError(t, store.QueryRow(ctx,
				"SELECT available_at - updated_at FROM bot_reply_outbox WHERE id = ?1", claim.ID,
			).Scan(&delayMS))

			persistedDelay := time.Duration(delayMS) * time.Millisecond
			assert.Greater(t, persistedDelay, previousDelay, "available_at delay must increase with attempts")

			previousDelay = persistedDelay

			immediate, claimErr := repo.Claim(ctx, "too-early", durabilityTestLease)
			require.NoError(t, claimErr)
			assert.Nil(t, immediate, "outcome_unknown retry must honor durable backoff")

			_, err = store.Exec(ctx, "UPDATE bot_reply_outbox SET row_version = row_version + 1, available_at = ?2 WHERE id = ?1", claim.ID, time.Now().UnixMilli())
			require.NoError(t, err)
		}
	}

	status, payload, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)

	var attempts int32

	require.NoError(t, store.QueryRow(ctx, "SELECT attempts FROM bot_reply_outbox WHERE client_request_id = ?1", entry.ClientRequestID).Scan(&attempts))
	assert.Equal(t, ReplyOutboxManualReview, status)
	assert.Equal(t, int32(5), attempts)
	assert.NotNil(t, payload, "manual review must preserve the original payload")

	next, err := repo.Claim(ctx, "token-6", durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, next)
}

func TestReplyOutboxAutomaticReplayStopsAtSafetyBoundaries(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	t.Run("replay horizon moves an ambiguous row to manual review", func(t *testing.T) {
		assertReplayHorizonSendsAmbiguousRowToManualReview(ctx, t, store, repo)
	})

	t.Run("an ambiguous row before the replay horizon remains claimable", func(t *testing.T) {
		assertAmbiguousRowBeforeHorizonStaysClaimable(ctx, t, store, repo)
	})

	t.Run("the final crashed attempt is not requeued", func(t *testing.T) {
		assertFinalCrashedAttemptIsNotRequeued(ctx, t, store, repo)
	})
}

func assertReplayHorizonSendsAmbiguousRowToManualReview(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:m-replay-horizon", 0, `{"body":"answer"}`)
	claim := claimOne(ctx, t, repo, entry)
	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID: claim.ID, ClaimToken: testClaimToken, Status: ReplyOutboxOutcomeUnknown,
		RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)

	_, err = store.Exec(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1,
		    first_attempt_at = ?3 - ?2,
		    available_at = ?3
		WHERE id = ?1`, claim.ID, repo.automaticReplayHorizon.Milliseconds(), time.Now().UnixMilli())
	require.NoError(t, err)

	resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, resend, "the automatic path must fail closed at the replay horizon")

	status, payload, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxManualReview, status)
	assert.NotNil(t, payload)
}

func assertAmbiguousRowBeforeHorizonStaysClaimable(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:m-before-replay-horizon", 0, `{"body":"answer"}`)
	claim := claimOne(ctx, t, repo, entry)
	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID: claim.ID, ClaimToken: testClaimToken, Status: ReplyOutboxOutcomeUnknown,
		RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)

	_, err = store.Exec(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1,
		    first_attempt_at = ?2,
		    available_at = ?3
		WHERE id = ?1`, claim.ID, time.Now().Add(-143*time.Hour).UnixMilli(), time.Now().UnixMilli())
	require.NoError(t, err)

	resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, resend)
	assert.Equal(t, int32(2), resend.Attempts)
}

func expireReplyLease(ctx context.Context, t *testing.T, store *Store, clientRequestID string) {
	t.Helper()

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version = row_version + 1, lease_until = lease_until - 1 WHERE client_request_id = ?1`, clientRequestID)
	require.NoError(t, err)
}

func assertFinalCrashedAttemptIsNotRequeued(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry("message:m-attempt-boundary", 0, `{"body":"answer"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version = row_version + 1, attempts = ?2 - 1 WHERE client_request_id = ?1`,
		entry.ClientRequestID, repo.maxAttempts)
	require.NoError(t, err)

	claim, err := repo.Claim(ctx, "token-final", time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, repo.maxAttempts, claim.Attempts)
	expireReplyLease(ctx, t, store, entry.ClientRequestID)

	reclaim, err := repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), reclaim.Requeued)
	assert.Equal(t, int64(1), reclaim.SafetyManualReview)

	status, _, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxManualReview, status)
}

func mustInsertReply(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, entry *ReplyOutboxEntry) ReplyOutboxInsertOutcome {
	t.Helper()

	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)

	return outcome
}

func TestReplyOutboxExpiredLeaseRecovers(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	t.Run("an expired submitting lease returns the stored payload to the queue", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, store)

		entry := newReplyOutboxEntry("message:m-expired", 0, `{"body":"answer"}`)
		inserted, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, inserted)

		_, err = repo.Claim(ctx, testClaimToken, time.Millisecond)
		require.NoError(t, err)
		expireReplyLease(ctx, t, store, entry.ClientRequestID)

		reclaim, err := repo.ReclaimExpired(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Requeued)

		resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, resend)
		assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID)
		assert.JSONEq(t, `{"body":"answer"}`, string(resend.Payload))
	})
}

func TestReplyOutboxTerminalSettlementDropsTheBody(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	t.Run("a completed handoff keeps the identity row without the body", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, store)

		entry := newReplyOutboxEntry("message:m-retention", 0, `{"body":"사용자 원문이 섞인 응답"}`)
		claim := claimOne(ctx, t, repo, entry)

		completeReplyHandoff(ctx, t, repo, claim)

		var (
			payload         []byte
			clientRequestID string
		)

		require.NoError(t, store.QueryRow(ctx,
			"SELECT payload, client_request_id FROM bot_reply_outbox WHERE id = ?1", claim.ID).
			Scan(&payload, &clientRequestID))
		assert.Nil(t, payload, "replay 창이 닫히면 본문은 retention 기간 동안 남을 이유가 없다")
		assert.Equal(t, entry.ClientRequestID, clientRequestID)
	})

	t.Run("a resendable settlement keeps the body", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, store)

		entry := newReplyOutboxEntry("message:m-keep", 0, `{"body":"answer"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: testClaimToken,
			Status:     ReplyOutboxOutcomeUnknown,
		})
		require.NoError(t, err)
		require.True(t, applied)

		var payload []byte

		require.NoError(t, store.QueryRow(ctx,
			"SELECT payload FROM bot_reply_outbox WHERE id = ?1", claim.ID).Scan(&payload))
		assert.NotNil(t, payload, "재발송이 남아 있으면 본문을 지우면 안 된다")
	})

	t.Run("manual review keeps the body for the human", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, store)

		entry := newReplyOutboxEntry("message:m-review", 0, `{"body":"answer"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: testClaimToken,
			Status:     ReplyOutboxManualReview,
		})
		require.NoError(t, err)
		require.True(t, applied)

		var payload []byte

		require.NoError(t, store.QueryRow(ctx,
			"SELECT payload FROM bot_reply_outbox WHERE id = ?1", claim.ID).Scan(&payload))
		assert.NotNil(t, payload)
	})
}
