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
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInboxClaimDoesNotScanBlockedFollowers(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewInboxRepository(pool)
	admitOne(ctx, t, repo, inboxMessage("message:head-plan", "room-1", "room:room-1"))
	_, err := pool.Exec(ctx, `INSERT INTO bot_webhook_inbox(message_id, room_id, ordering_key, payload)
		SELECT 'message:follower-' || n, 'room-1', 'room:room-1', '{"JSON":{"message_id":"follower"}}'::jsonb
		FROM generate_series(1, 5000) AS n`)
	require.NoError(t, err)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()
	var planJSON []byte
	err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+inboxClaimSQL, "plan-token", int64(durabilityTestLease/time.Millisecond)).Scan(&planJSON)
	require.NoError(t, err)
	var plan []map[string]any
	require.NoError(t, json.Unmarshal(planJSON, &plan))
	require.NotEmpty(t, plan)
	stats := inboxPlanStats(plan[0])
	assert.False(t, stats.SeqScan, "claim plan must not sequentially scan the inbox")
	assert.True(t, stats.IndexLookup, "claim plan must use an inbox index lookup")
	assert.LessOrEqual(t, stats.ExaminedRows, float64(4), "claim plan examined blocked follower rows")
}

type claimPlanStats struct {
	ExaminedRows float64
	SeqScan      bool
	IndexLookup  bool
}

func inboxPlanStats(node map[string]any) claimPlanStats {
	plan := planMapValue(node, "Plan")
	if plan != nil {
		return inboxPlanStats(plan)
	}
	stats := claimPlanStats{}
	if node["Relation Name"] == "bot_webhook_inbox" {
		loops := planFloatValue(node, "Actual Loops")
		if loops == 0 {
			loops = 1
		}
		actual := planFloatValue(node, "Actual Rows")
		filtered := planFloatValue(node, "Rows Removed by Filter")
		rechecked := planFloatValue(node, "Rows Removed by Index Recheck")
		stats.ExaminedRows = (actual + filtered + rechecked) * loops
		nodeType := planStringValue(node, "Node Type")
		stats.SeqScan = nodeType == "Seq Scan"
		stats.IndexLookup = nodeType == "Index Scan" || nodeType == "Index Only Scan" || nodeType == "Bitmap Heap Scan"
	}
	children := planSliceValue(node, "Plans")
	for _, child := range children {
		childNode := planNodeValue(child)
		childStats := inboxPlanStats(childNode)
		stats.ExaminedRows += childStats.ExaminedRows
		stats.SeqScan = stats.SeqScan || childStats.SeqScan
		stats.IndexLookup = stats.IndexLookup || childStats.IndexLookup
	}
	return stats
}

func planMapValue(node map[string]any, key string) map[string]any {
	value, ok := node[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func planNodeValue(value any) map[string]any {
	node, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return node
}

func planFloatValue(node map[string]any, key string) float64 {
	value, ok := node[key].(float64)
	if !ok {
		return 0
	}
	return value
}

func planStringValue(node map[string]any, key string) string {
	value, ok := node[key].(string)
	if !ok {
		return ""
	}
	return value
}

func planSliceValue(node map[string]any, key string) []any {
	value, ok := node[key].([]any)
	if !ok {
		return nil
	}
	return value
}

func TestInboxConcurrentFirstAdmitsPreserveOneOldestHead(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewInboxRepository(pool)
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, inboxOrderingKeyLockSQL, "room:barrier-first")
	require.NoError(t, err)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, id := range []string{"message:first-a", "message:first-b"} {
		go func() {
			<-start
			_, admitErr := repo.Admit(ctx, inboxMessage(id, "room-1", "room:barrier-first"))
			errs <- admitErr
		}()
	}
	close(start)
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	var head, oldest string
	require.NoError(t, pool.QueryRow(ctx, "SELECT message_id FROM bot_webhook_heads WHERE ordering_key = 'room:barrier-first'").Scan(&head))
	require.NoError(t, pool.QueryRow(ctx, "SELECT message_id FROM bot_webhook_inbox WHERE ordering_key = 'room:barrier-first' ORDER BY id LIMIT 1").Scan(&oldest))
	assert.Equal(t, oldest, head)
}

func TestInboxAdmitRacingTerminalTransitionPreservesSuccessorHead(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewInboxRepository(pool)
	admitOne(ctx, t, repo, inboxMessage("message:terminal-head", "room-1", "room:barrier-terminal"))
	claim, err := repo.Claim(ctx, "terminal-token", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, inboxOrderingKeyLockSQL, "room:barrier-terminal")
	require.NoError(t, err)
	errs := make(chan error, 2)
	go func() { _, e := repo.Abandon(ctx, claim.MessageID, "terminal-token", "poison"); errs <- e }()
	go func() {
		_, e := repo.Admit(ctx, inboxMessage("message:successor", "room-1", "room:barrier-terminal"))
		errs <- e
	}()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	var head string
	require.NoError(t, pool.QueryRow(ctx, "SELECT message_id FROM bot_webhook_heads WHERE ordering_key = 'room:barrier-terminal'").Scan(&head))
	assert.Equal(t, "message:successor", head)
}

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
		assert.Equal(t, int64(1), reclaim.AcceptedManualReview)
		assert.Equal(t, int64(0), reclaim.Requeued)

		status, payload, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxManualReview, status)
		assert.NotNil(t, payload)
	})
}

func TestReplyOutboxClaimsPreserveRoomOrderWhileOtherRoomsProgress(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	roomAHead := newReplyOutboxEntry("message:room-a-head", 0, `{"kind":"text","message":"a1"}`)
	roomAFollower := newReplyOutboxEntry("message:room-a-follower", 0, `{"kind":"text","message":"a2"}`)
	roomAFollower.RoomID = roomAHead.RoomID
	roomB := newReplyOutboxEntry("message:room-b", 0, `{"kind":"text","message":"b1"}`)
	roomB.RoomID = "room-b"
	for _, entry := range []*ReplyOutboxEntry{roomAHead, roomAFollower, roomB} {
		_, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
	}

	first, err := repo.Claim(ctx, "worker-a", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, roomAHead.MessageID, first.MessageID)

	second, err := repo.Claim(ctx, "worker-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, roomB.MessageID, second.MessageID,
		"a concurrent worker must progress another room instead of overtaking the claimed room head")

	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID: first.ID, ClaimToken: "worker-a", Status: ReplyOutboxManualReview,
		LastError: "operator decision required",
	})
	require.NoError(t, err)
	require.True(t, applied)

	third, err := repo.Claim(ctx, "worker-c", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, third)
	assert.Equal(t, roomAFollower.MessageID, third.MessageID,
		"manual review must not permanently block later replies in the room")
}

func TestInboxPoisonTerminalTransitionRacesHeadOfLineClaims(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	const workers = 16
	poison := inboxMessage("message:poison-hammer", "room-1", "room:room-1")
	healthy := inboxMessage("message:healthy-hammer", "room-1", "room:room-1")
	admitOne(ctx, t, repo, poison)
	admitOne(ctx, t, repo, healthy)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := range workers {
		wg.Go(func() {
			<-start
			token := fmt.Sprintf("hammer-%d", worker)
			claim, err := repo.Claim(ctx, token, durabilityTestLease)
			if err != nil || claim == nil {
				if err != nil {
					errs <- err
				}
				return
			}
			switch claim.MessageID {
			case poison.MessageID:
				_, err = repo.Abandon(ctx, claim.MessageID, token, "stored payload is not decodable")
			case healthy.MessageID:
				_, err = repo.Complete(ctx, claim.MessageID, token)
			default:
				err = fmt.Errorf("unexpected claim %q", claim.MessageID)
			}
			if err != nil {
				errs <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	status, _, _, _ := inboxRow(ctx, t, pool, poison.MessageID)
	assert.Equal(t, "dead", status)
	claim, err := repo.Claim(ctx, "final-worker", durabilityTestLease)
	require.NoError(t, err)
	if claim != nil {
		assert.Equal(t, healthy.MessageID, claim.MessageID)
	}
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
