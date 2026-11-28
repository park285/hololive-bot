package durability

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInboxClaimDoesNotScanBlockedFollowers(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := NewInboxRepository(store)
	admitOne(ctx, t, repo, inboxMessage("message:head-plan", "room-1", "room:room-1"))

	now := time.Now().UnixMilli()

	_, err := store.Exec(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5000)
		INSERT INTO bot_webhook_inbox(message_id, room_id, ordering_key, payload, available_at, created_at, updated_at)
		SELECT 'message:follower-' || i, 'room-1', 'room:room-1', '{"JSON":{"message_id":"follower"}}', ?1, ?1, ?1
		FROM n`, now)
	require.NoError(t, err)

	_, err = store.Exec(ctx, "ANALYZE")
	require.NoError(t, err)

	details := explainQueryPlan(ctx, t, store, inboxClaimSQL, now, "plan-token", durabilityTestLease.Milliseconds())

	for _, detail := range details {
		assert.False(t, strings.HasPrefix(detail, "SCAN due") || strings.HasPrefix(detail, "SCAN bot_webhook_inbox"),
			"claim plan must not scan blocked followers: %v", details)
	}

	assert.True(t, usesNamedIndex(details, "due"), "claim plan must look up the head message by index: %v", details)
}

func TestInboxConcurrentFirstAdmitsPreserveOneOldestHead(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := NewInboxRepository(store)
	start := make(chan struct{})
	errs := make(chan error, 2)

	for _, id := range []string{"message:first-a", "message:first-b"} {
		go func() {
			<-start

			_, admitErr := repo.AdmitResult(ctx, inboxMessage(id, "room-1", "room:barrier-first"))
			errs <- admitErr
		}()
	}

	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	var head, oldest string

	require.NoError(t, store.QueryRow(ctx, "SELECT message_id FROM bot_webhook_heads WHERE ordering_key = 'room:barrier-first'").Scan(&head))
	require.NoError(t, store.QueryRow(ctx, "SELECT message_id FROM bot_webhook_inbox WHERE ordering_key = 'room:barrier-first' ORDER BY id LIMIT 1").Scan(&oldest))
	assert.Equal(t, oldest, head)
}

func TestInboxAdmitRacingTerminalTransitionPreservesSuccessorHead(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := NewInboxRepository(store)
	admitOne(ctx, t, repo, inboxMessage("message:terminal-head", "room-1", "room:barrier-terminal"))

	claim, err := repo.Claim(ctx, "terminal-token", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	start := make(chan struct{})
	errs := make(chan error, 2)

	go func() {
		<-start

		_, e := repo.Abandon(ctx, claim.MessageID, "terminal-token", "poison")
		errs <- e
	}()
	go func() {
		<-start

		_, e := repo.AdmitResult(ctx, inboxMessage("message:successor", "room-1", "room:barrier-terminal"))
		errs <- e
	}()

	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	var head string

	require.NoError(t, store.QueryRow(ctx, "SELECT message_id FROM bot_webhook_heads WHERE ordering_key = 'room:barrier-terminal'").Scan(&head))
	assert.Equal(t, "message:successor", head)
}

func TestReplyOutboxAcceptedNeverReturnsToTheClaimQueue(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	for _, status := range []string{ReplyOutboxOutcomeUnknown, ReplyOutboxRetryablePreDispatch} {
		t.Run(status, func(t *testing.T) {
			truncateDurabilityTables(ctx, t, store)

			entry := newReplyOutboxEntry("message:m-"+status, 0, `{"body":"delivered"}`)
			claim := claimOne(ctx, t, repo, entry)

			accepted, err := repo.SettleAccepted(ctx, claim.ID, testClaimToken, "iris-req-1", time.Minute)
			require.NoError(t, err)
			require.True(t, accepted)

			applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
				ID:         claim.ID,
				ClaimToken: testClaimToken,
				Status:     status,
				LastError:  "settled after Iris already accepted the reply",
			})
			require.NoError(t, err)
			assert.False(t, applied,
				"Iris가 수리한 뒤 재발송 가능 status로 정산되면 admission TTL이 지난 시점에 중복 발화가 된다")

			rowStatus, _, _, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
			assert.Equal(t, ReplyOutboxAccepted, rowStatus)

			resend, err := repo.Claim(ctx, "token-b", durabilityTestLease)
			require.NoError(t, err)
			assert.Nil(t, resend, "발송된 행이 claim 큐로 돌아오면 중복 발화가 된다")
		})
	}

	t.Run("lease reclaim leaves an unleased accepted row to the handoff reconciler", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, store)

		entry := newReplyOutboxEntry("message:m-unleased", 0, `{"body":"delivered"}`)
		inserted, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, inserted)

		claim, err := repo.Claim(ctx, testClaimToken, time.Millisecond)
		require.NoError(t, err)
		require.NotNil(t, claim)

		accepted, err := repo.SettleAccepted(ctx, claim.ID, testClaimToken, "iris-req-1", time.Minute)
		require.NoError(t, err)
		require.True(t, accepted)

		var unleased bool

		require.NoError(t, store.QueryRow(ctx, `SELECT lease_until IS NULL FROM bot_reply_outbox WHERE id = ?1`, claim.ID).Scan(&unleased))
		require.True(t, unleased)

		reclaim, err := repo.ReclaimExpired(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxReclaim{}, reclaim)

		status, payload, _, token := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxAccepted, status)
		assert.NotNil(t, payload, "failed 재발송에 쓸 payload는 handoff 확인 전까지 남는다")
		assert.Nil(t, token)
	})
}

func TestReplyOutboxClaimsPreserveRoomOrderWhileOtherRoomsProgress(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

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
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

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

	status, _, _, _ := inboxRow(ctx, t, store, poison.MessageID)
	assert.Equal(t, "dead", status)

	claim, err := repo.Claim(ctx, "final-worker", durabilityTestLease)
	require.NoError(t, err)

	if claim != nil {
		assert.Equal(t, healthy.MessageID, claim.MessageID)
	}
}

const (
	poisonMaxAttempts = int32(3)
	poisonOrderingKey = "room:room-1"
)

func TestInboxPoisonMessageReachesTerminalAndUnblocksTheRoom(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()

	t.Run("cooperative release stops retrying once attempts are exhausted", func(t *testing.T) {
		assertCooperativeReleaseReachesTerminal(ctx, t, store, repo)
	})

	t.Run("a worker can declare a permanent failure without burning attempts", func(t *testing.T) {
		assertAbandonBypassesTheAttemptBudget(ctx, t, store, repo)
	})

	t.Run("invalid arguments are rejected before touching the store", func(t *testing.T) {
		assertInboxTerminalRejectsInvalidArguments(ctx, t, repo)
	})
}

func assertCooperativeReleaseReachesTerminal(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	poison := inboxMessage("message:poison", "room-1", poisonOrderingKey)
	healthy := inboxMessage("message:healthy", "room-1", poisonOrderingKey)

	admitOne(ctx, t, repo, poison)
	admitOne(ctx, t, repo, healthy)

	outcome := releaseUntilTerminal(ctx, t, repo, poison)
	require.Equal(t, InboxReleaseAbandoned, outcome,
		"협조적 Release만 하는 워커에서는 ReclaimExpired가 영원히 안 도므로 여기서 종단에 닿아야 한다")

	status, _, attempts, _ := inboxRow(ctx, t, store, poison.MessageID)
	assert.Equal(t, "dead", status)
	assert.LessOrEqual(t, attempts, poisonMaxAttempts)

	claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim, "poison이 종단에 닿았으면 뒤 메시지가 진행돼야 한다")
	assert.Equal(t, healthy.MessageID, claim.MessageID)
}

func releaseUntilTerminal(
	ctx context.Context,
	t *testing.T,
	repo *InboxRepository,
	poison InboxMessage,
) InboxReleaseOutcome {
	t.Helper()

	var outcome InboxReleaseOutcome

	for round := range 8 {
		claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
		require.NoError(t, err)

		if claim == nil {
			break
		}

		require.Equal(t, poison.MessageID, claim.MessageID,
			"head-of-line 순서 보장은 poison이 종단에 닿기 전까지 유지돼야 한다")

		outcome, err = repo.Release(ctx, poison.MessageID, testClaimToken, claim.Attempts, poisonMaxAttempts,
			time.Millisecond, fmt.Sprintf("boom %d", round))
		require.NoError(t, err)

		if outcome == InboxReleaseAbandoned {
			break
		}

		require.Equal(t, InboxReleaseRetried, outcome)

		_, err = repo.store.Exec(ctx, `UPDATE bot_webhook_inbox SET available_at = available_at - 1 WHERE message_id = ?1`, poison.MessageID)
		require.NoError(t, err)
	}

	return outcome
}

func assertAbandonBypassesTheAttemptBudget(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	poison := inboxMessage("message:unparseable", "room-1", poisonOrderingKey)
	healthy := inboxMessage("message:next", "room-1", poisonOrderingKey)

	admitOne(ctx, t, repo, poison)
	admitOne(ctx, t, repo, healthy)

	_, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)

	applied, err := repo.Abandon(ctx, poison.MessageID, "token-stale", "payload is not decodable")
	require.NoError(t, err)
	assert.False(t, applied, "종단 전이도 claim token으로 펜싱돼야 한다")

	applied, err = repo.Abandon(ctx, poison.MessageID, testClaimToken, "payload is not decodable")
	require.NoError(t, err)
	require.True(t, applied)

	status, _, _, token := inboxRow(ctx, t, store, poison.MessageID)
	assert.Equal(t, "dead", status)
	assert.Nil(t, token)

	claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, healthy.MessageID, claim.MessageID)
}

func assertInboxTerminalRejectsInvalidArguments(ctx context.Context, t *testing.T, repo *InboxRepository) {
	t.Helper()

	_, err := repo.Release(ctx, "message:x", testClaimToken, 1, 0, time.Minute, "boom")
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = repo.Release(ctx, "message:x", testClaimToken, 1, 3, 500*time.Microsecond, "boom")
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = repo.Abandon(ctx, "message:x", "  ", "reason")
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = repo.Abandon(ctx, "message:x", testClaimToken, "   ")
	require.ErrorIs(t, err, ErrInvalidArgument)
}
