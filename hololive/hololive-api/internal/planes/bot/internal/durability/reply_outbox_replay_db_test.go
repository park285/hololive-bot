// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package durability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyOutboxDivergentReplayDoesNotBlockDelivery(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	stored := newReplyOutboxEntry("message:m-replay", 0, `{"body":"error notice"}`)
	outcome, err := repo.Insert(ctx, stored)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	recomputed := *stored
	recomputed.Payload = []byte(`{"body":"the real answer"}`)
	outcome, err = repo.Insert(ctx, &recomputed)
	require.NoError(t, err, "payload 불일치는 전달을 막는 실패가 아니라 관측 신호여야 한다")
	assert.Equal(t, ReplyOutboxPayloadDiverged, outcome)

	claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim, "재계산 바이트가 달라도 저장본은 계속 발송 가능해야 한다")
	assert.JSONEq(t, `{"body":"error notice"}`, string(claim.Payload))
	assert.Equal(t, stored.ClientRequestID, claim.ClientRequestID)
}

func TestReplyOutboxOutcomeUnknownIsResendable(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	entry := newReplyOutboxEntry("message:m-unknown", 0, `{"body":"answer"}`)
	claim := claimOne(ctx, t, repo, entry)

	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID:         claim.ID,
		ClaimToken: "token-a",
		Status:     ReplyOutboxOutcomeUnknown,
		LastError:  "network reset before the outcome was known",
	})
	require.NoError(t, err)
	require.True(t, applied)

	resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, resend, "outcome_unknown이 재발송 불가면 사용자는 아무것도 받지 못한다")
	assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID,
		"같은 clientRequestId여야 Iris admission dedup이 중복을 흡수한다")
	assert.JSONEq(t, `{"body":"answer"}`, string(resend.Payload))
}

func TestReplyOutboxExpiredLeaseRecovers(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	t.Run("an expired submitting lease returns the stored payload to the queue", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-expired", 0, `{"body":"answer"}`)
		inserted, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, inserted)

		_, err = repo.Claim(ctx, "token-a", time.Millisecond)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		reclaim, err := repo.ReclaimExpired(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Requeued)

		resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, resend)
		assert.Equal(t, entry.ClientRequestID, resend.ClientRequestID)
		assert.JSONEq(t, `{"body":"answer"}`, string(resend.Payload))
	})

	t.Run("an expired accepted lease is absorbed as a completed handoff", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-accepted-expired", 0, `{"body":"answer"}`)
		claim := claimOneWithLease(ctx, t, repo, entry, time.Millisecond)

		accepted, err := repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-req-1")
		require.NoError(t, err)
		require.True(t, accepted)
		time.Sleep(20 * time.Millisecond)

		reclaim, err := repo.ReclaimExpired(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Absorbed)
		assert.Equal(t, int64(0), reclaim.Requeued)

		status, _, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxHandoffCompleted, status)

		resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, resend, "Iris가 수리한 응답을 다시 보내면 중복 발화다")
	})
}

func TestReplyOutboxTerminalSettlementDropsTheBody(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	t.Run("a completed handoff keeps the identity row without the body", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-retention", 0, `{"body":"사용자 원문이 섞인 응답"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxHandoffCompleted,
		})
		require.NoError(t, err)
		require.True(t, applied)

		var payload []byte
		var clientRequestID string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT payload, client_request_id FROM bot_reply_outbox WHERE id = $1", claim.ID).
			Scan(&payload, &clientRequestID))
		assert.Nil(t, payload, "replay 창이 닫히면 본문은 retention 기간 동안 남을 이유가 없다")
		assert.Equal(t, entry.ClientRequestID, clientRequestID)
	})

	t.Run("a resendable settlement keeps the body", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-keep", 0, `{"body":"answer"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxOutcomeUnknown,
		})
		require.NoError(t, err)
		require.True(t, applied)

		var payload []byte
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT payload FROM bot_reply_outbox WHERE id = $1", claim.ID).Scan(&payload))
		assert.NotNil(t, payload, "재발송이 남아 있으면 본문을 지우면 안 된다")
	})

	t.Run("manual review keeps the body for the human", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-review", 0, `{"body":"answer"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxManualReview,
		})
		require.NoError(t, err)
		require.True(t, applied)

		var payload []byte
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT payload FROM bot_reply_outbox WHERE id = $1", claim.ID).Scan(&payload))
		assert.NotNil(t, payload)
	})
}

func claimOneWithLease(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, entry *ReplyOutboxEntry, lease time.Duration) *ReplyOutboxClaim {
	t.Helper()

	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	claim, err := repo.Claim(ctx, "token-a", lease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	return claim
}
