package durability

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const headAdvanceOrderingKey = "room:head-advance"

type inboxHeadTransition struct {
	name     string
	terminal bool
	apply    func(t *testing.T, claim *InboxClaim)
}

func TestInboxTransitionsMoveTheOrderingHeadOnlyWhenTerminal(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()

	for _, transition := range inboxHeadTransitions(ctx, store, repo) {
		t.Run(transition.name+" with a successor", func(t *testing.T) {
			truncateDurabilityTables(ctx, t, store)

			head := inboxMessage("message:head-advance-first", "room-1", headAdvanceOrderingKey)
			successor := inboxMessage("message:head-advance-second", "room-1", headAdvanceOrderingKey)

			admitOne(ctx, t, repo, head)
			admitOne(ctx, t, repo, successor)

			transition.apply(t, claimHeadAdvanceMessage(ctx, t, repo, head))

			want := head.MessageID

			if transition.terminal {
				want = successor.MessageID
			}

			got, ok := webhookHead(ctx, t, store, headAdvanceOrderingKey)
			require.True(t, ok, "active successor must keep an ordering head")
			assert.Equal(t, want, got)
		})

		t.Run(transition.name+" without a successor", func(t *testing.T) {
			truncateDurabilityTables(ctx, t, store)

			head := inboxMessage("message:head-advance-only", "room-1", headAdvanceOrderingKey)
			admitOne(ctx, t, repo, head)

			transition.apply(t, claimHeadAdvanceMessage(ctx, t, repo, head))

			got, ok := webhookHead(ctx, t, store, headAdvanceOrderingKey)

			if transition.terminal {
				assert.False(t, ok, "terminal head without successor must release the ordering key, got %q", got)

				return
			}

			require.True(t, ok, "requeued head must stay the ordering head")
			assert.Equal(t, head.MessageID, got)
		})
	}
}

func inboxHeadTransitions(ctx context.Context, store *Store, repo *InboxRepository) []inboxHeadTransition {
	return []inboxHeadTransition{
		{name: "complete", terminal: true, apply: func(t *testing.T, claim *InboxClaim) {
			t.Helper()

			applied, err := repo.Complete(ctx, claim.MessageID, testClaimToken)
			require.NoError(t, err)
			require.True(t, applied)
		}},
		{name: "abandon", terminal: true, apply: func(t *testing.T, claim *InboxClaim) {
			t.Helper()

			applied, err := repo.Abandon(ctx, claim.MessageID, testClaimToken, "poison")
			require.NoError(t, err)
			require.True(t, applied)
		}},
		{name: "release after max attempts", terminal: true, apply: func(t *testing.T, claim *InboxClaim) {
			t.Helper()

			outcome, err := repo.Release(ctx, claim.MessageID, testClaimToken, claim.Attempts, claim.Attempts,
				time.Millisecond, "failure")
			require.NoError(t, err)
			require.Equal(t, InboxReleaseAbandoned, outcome)
		}},
		{name: "reclaim after max attempts", terminal: true, apply: func(t *testing.T, claim *InboxClaim) {
			t.Helper()

			expireInboxLease(ctx, t, store, claim.MessageID)

			reclaim, err := repo.ReclaimExpired(ctx, claim.Attempts, 100)
			require.NoError(t, err)
			require.Equal(t, InboxReclaim{Abandoned: 1}, reclaim)
		}},
		{name: "reclaim with attempts left", terminal: false, apply: func(t *testing.T, claim *InboxClaim) {
			t.Helper()

			expireInboxLease(ctx, t, store, claim.MessageID)

			reclaim, err := repo.ReclaimExpired(ctx, claim.Attempts+1, 100)
			require.NoError(t, err)
			require.Equal(t, InboxReclaim{Requeued: 1}, reclaim)
		}},
	}
}

func claimHeadAdvanceMessage(ctx context.Context, t *testing.T, repo *InboxRepository, want InboxMessage) *InboxClaim {
	t.Helper()

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, want.MessageID, claim.MessageID)

	return claim
}

func expireInboxLease(ctx context.Context, t *testing.T, store *Store, messageID string) {
	t.Helper()

	_, err := store.Exec(ctx,
		"UPDATE bot_webhook_inbox SET lease_until = ?2 WHERE message_id = ?1",
		messageID, time.Now().Add(-time.Minute).UnixMilli())
	require.NoError(t, err)
}

func webhookHead(ctx context.Context, t *testing.T, store *Store, orderingKey string) (string, bool) {
	t.Helper()

	var head string

	err := store.QueryRow(ctx, "SELECT message_id FROM bot_webhook_heads WHERE ordering_key = ?1", orderingKey).Scan(&head)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	require.NoError(t, err)

	return head, true
}

func TestInboxSettlementOfAnUnknownMessageIsNotOwned(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	const missing = "message:never-admitted"

	applied, err := repo.Complete(ctx, missing, testClaimToken)
	require.NoError(t, err)
	assert.False(t, applied)

	applied, err = repo.Abandon(ctx, missing, testClaimToken, "poison")
	require.NoError(t, err)
	assert.False(t, applied)

	outcome, err := repo.Release(ctx, missing, testClaimToken, 1, 3, time.Millisecond, "failure")
	require.NoError(t, err)
	assert.Equal(t, InboxReleaseNotOwned, outcome)
}
