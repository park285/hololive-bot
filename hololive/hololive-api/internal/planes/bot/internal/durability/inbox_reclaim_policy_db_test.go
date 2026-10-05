package durability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInboxReclaimSkipsLockedHeadAndAppliesMixedPolicies(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := t.Context()

	for _, id := range []string{"exhausted", "retryable", "live"} {
		admitOne(ctx, t, repo, inboxMessage("message:"+id, id, "room:"+id))

		claim, err := repo.Claim(ctx, id+"-token", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
	}

	admitOne(ctx, t, repo, inboxMessage("message:successor", "exhausted", "room:exhausted"))

	_, err := pool.Exec(ctx, `UPDATE bot_webhook_inbox SET lease_until=clock_timestamp()-interval '1 minute',
		attempts=CASE WHEN message_id='message:exhausted' THEN 3 ELSE 1 END
		WHERE message_id IN ('message:exhausted','message:retryable')`)
	require.NoError(t, err)

	locker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, rollbackInboxTx(ctx, locker)) }()

	_, err = locker.Exec(ctx, `SELECT message_id FROM bot_webhook_inbox WHERE message_id='message:exhausted' FOR UPDATE`)
	require.NoError(t, err)

	result, err := repo.ReclaimExpired(ctx, 3, 100)
	require.NoError(t, err)
	require.Equal(t, InboxReclaim{Requeued: 1}, result)

	status, _, _, _ := inboxRow(ctx, t, pool, "message:live")
	require.Equal(t, "processing", status)
	require.NoError(t, locker.Commit(ctx))

	result, err = repo.ReclaimExpired(ctx, 3, 100)
	require.NoError(t, err)
	require.Equal(t, InboxReclaim{Abandoned: 1}, result)

	status, _, _, _ = inboxRow(ctx, t, pool, "message:exhausted")
	require.Equal(t, "dead", status)

	var head string

	require.NoError(t, pool.QueryRow(ctx, `SELECT message_id FROM bot_webhook_heads WHERE ordering_key='room:exhausted'`).Scan(&head))
	require.Equal(t, "message:successor", head)

	result, err = repo.ReclaimExpired(ctx, 3, 100)
	require.NoError(t, err)
	require.Equal(t, InboxReclaim{}, result)
}
