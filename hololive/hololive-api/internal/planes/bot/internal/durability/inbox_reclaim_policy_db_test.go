package durability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInboxReclaimAppliesMixedPolicies(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()

	for _, id := range []string{"exhausted", "retryable", "live"} {
		admitOne(ctx, t, repo, inboxMessage("message:"+id, id, "room:"+id))

		claim, err := repo.Claim(ctx, id+"-token", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
	}

	admitOne(ctx, t, repo, inboxMessage("message:successor", "exhausted", "room:exhausted"))

	_, err := store.Exec(ctx, `UPDATE bot_webhook_inbox SET lease_until = ?1,
		attempts=CASE WHEN message_id='message:exhausted' THEN 3 ELSE 1 END
		WHERE message_id IN ('message:exhausted','message:retryable')`, time.Now().Add(-time.Minute).UnixMilli())
	require.NoError(t, err)

	result, err := repo.ReclaimExpired(ctx, 3, 100)
	require.NoError(t, err)
	require.Equal(t, InboxReclaim{Requeued: 1, Abandoned: 1}, result)

	status, _, _, _ := inboxRow(ctx, t, store, "message:live")
	require.Equal(t, "processing", status)

	status, _, _, _ = inboxRow(ctx, t, store, "message:exhausted")
	require.Equal(t, "dead", status)

	status, _, _, _ = inboxRow(ctx, t, store, "message:retryable")
	require.Equal(t, "retry", status)

	var head string

	require.NoError(t, store.QueryRow(ctx, `SELECT message_id FROM bot_webhook_heads WHERE ordering_key='room:exhausted'`).Scan(&head))
	require.Equal(t, "message:successor", head)

	result, err = repo.ReclaimExpired(ctx, 3, 100)
	require.NoError(t, err)
	require.Equal(t, InboxReclaim{}, result)
}
