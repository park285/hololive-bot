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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyOutboxAcceptedNeverReturnsToTheClaimQueue(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	for _, status := range []string{ReplyOutboxOutcomeUnknown, ReplyOutboxRetryablePreDispatch} {
		t.Run(status, func(t *testing.T) {
			truncateDurabilityTables(ctx, t, pool)
			entry := newReplyOutboxEntry("message:m-"+status, 0, `{"body":"delivered"}`)
			claim := claimOne(ctx, t, repo, entry)

			accepted, err := repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-req-1")
			require.NoError(t, err)
			require.True(t, accepted)

			applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
				ID:         claim.ID,
				ClaimToken: "token-a",
				Status:     status,
				LastError:  "settled after Iris already accepted the reply",
			})
			require.NoError(t, err)
			assert.False(t, applied,
				"Iris가 수리한 뒤 재발송 가능 status로 정산되면 admission TTL이 지난 시점에 중복 발화가 된다")

			rowStatus, _, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
			assert.Equal(t, replyOutboxStatusAccepted, rowStatus)

			resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
			require.NoError(t, err)
			assert.Nil(t, resend, "발송된 행이 claim 큐로 돌아오면 중복 발화가 된다")
		})
	}

	t.Run("lease expiry absorbs the accepted row instead of resending it", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-absorbed", 0, `{"body":"delivered"}`)
		inserted, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, inserted)

		claim, err := repo.Claim(ctx, "token-a", time.Millisecond)
		require.NoError(t, err)
		require.NotNil(t, claim)

		accepted, err := repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-req-1")
		require.NoError(t, err)
		require.True(t, accepted)
		time.Sleep(20 * time.Millisecond)

		reclaim, err := repo.ReclaimExpired(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Absorbed)
		assert.Equal(t, int64(0), reclaim.Requeued)

		status, payload, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxHandoffCompleted, status)
		assert.Nil(t, payload)
	})
}

func TestInboxPoisonMessageReachesTerminalAndUnblocksTheRoom(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()

	const maxAttempts = int32(3)
	const orderingKey = "room:room-1"

	t.Run("cooperative release stops retrying once attempts are exhausted", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		poison := inboxMessage("message:poison", "room-1", orderingKey)
		healthy := inboxMessage("message:healthy", "room-1", orderingKey)
		admitOne(ctx, t, repo, poison)
		admitOne(ctx, t, repo, healthy)

		var outcome InboxReleaseOutcome
		for round := range 8 {
			claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
			require.NoError(t, err)
			if claim == nil {
				break
			}
			require.Equal(t, poison.MessageID, claim.MessageID,
				"head-of-line 순서 보장은 poison이 종단에 닿기 전까지 유지돼야 한다")

			outcome, err = repo.Release(ctx, poison.MessageID, "token-a", maxAttempts,
				time.Millisecond, fmt.Sprintf("boom %d", round))
			require.NoError(t, err)
			if outcome == InboxReleaseAbandoned {
				break
			}
			require.Equal(t, InboxReleaseRetried, outcome)
			time.Sleep(10 * time.Millisecond)
		}
		require.Equal(t, InboxReleaseAbandoned, outcome,
			"협조적 Release만 하는 워커에서는 ReclaimExpired가 영원히 안 도므로 여기서 종단에 닿아야 한다")

		status, _, attempts, _ := inboxRow(ctx, t, pool, poison.MessageID)
		assert.Equal(t, "dead", status)
		assert.LessOrEqual(t, attempts, maxAttempts)

		claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim, "poison이 종단에 닿았으면 뒤 메시지가 진행돼야 한다")
		assert.Equal(t, healthy.MessageID, claim.MessageID)
	})

	t.Run("a worker can declare a permanent failure without burning attempts", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		poison := inboxMessage("message:unparseable", "room-1", orderingKey)
		healthy := inboxMessage("message:next", "room-1", orderingKey)
		admitOne(ctx, t, repo, poison)
		admitOne(ctx, t, repo, healthy)

		_, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)

		applied, err := repo.Abandon(ctx, poison.MessageID, "token-stale", "payload is not decodable")
		require.NoError(t, err)
		assert.False(t, applied, "종단 전이도 claim token으로 펜싱돼야 한다")

		applied, err = repo.Abandon(ctx, poison.MessageID, "token-a", "payload is not decodable")
		require.NoError(t, err)
		require.True(t, applied)

		status, _, _, token := inboxRow(ctx, t, pool, poison.MessageID)
		assert.Equal(t, "dead", status)
		assert.Nil(t, token)

		claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
		assert.Equal(t, healthy.MessageID, claim.MessageID)
	})

	t.Run("invalid arguments are rejected before touching postgres", func(t *testing.T) {
		_, err := repo.Release(ctx, "message:x", "token-a", 0, time.Minute, "boom")
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.Release(ctx, "message:x", "token-a", 3, 500*time.Microsecond, "boom")
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.Abandon(ctx, "message:x", "  ", "reason")
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.Abandon(ctx, "message:x", "token-a", "   ")
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}
