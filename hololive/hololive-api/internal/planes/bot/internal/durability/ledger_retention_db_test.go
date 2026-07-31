package durability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurableTerminalPayloadScrubAndRetention(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	inbox := NewInboxRepository(pool)
	commands := NewCommandExecutionRepository(pool)
	outbox := NewReplyOutboxRepository(pool)
	ledger := NewDurableLedgerRepository(pool)

	admitOne(ctx, t, inbox, inboxMessage("message:retention", "room-1", "room:retention"))
	claim, err := inbox.Claim(ctx, "inbox-token", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	claimed, err := commands.Claim(ctx, claim.MessageID, "webhook", "inbox-token")
	require.NoError(t, err)
	require.True(t, claimed)
	completed, err := commands.Complete(ctx, claim.MessageID, "inbox-token", CommandExecutionFailed)
	require.NoError(t, err)
	require.True(t, completed)
	completed, err = inbox.Complete(ctx, claim.MessageID, "inbox-token")
	require.NoError(t, err)
	require.True(t, completed)

	var payload string
	require.NoError(t, pool.QueryRow(ctx, `SELECT payload::text FROM bot_webhook_inbox WHERE message_id = $1`, claim.MessageID).Scan(&payload))
	assert.Equal(t, `{}`, payload)

	entry := newReplyOutboxEntry("message:retention-reply", 0, `{"kind":"text","message":"answer"}`)
	claimReply := claimOne(ctx, t, outbox, entry)
	settled, err := outbox.Settle(ctx, ReplyOutboxSettlement{
		ID: claimReply.ID, ClaimToken: "token-a", Status: ReplyOutboxHandoffCompleted,
	})
	require.NoError(t, err)
	require.True(t, settled)

	manual := newReplyOutboxEntry("message:retention-manual", 0, `{"kind":"text","message":"review"}`)
	manualClaim := claimOne(ctx, t, outbox, manual)
	settled, err = outbox.Settle(ctx, ReplyOutboxSettlement{
		ID: manualClaim.ID, ClaimToken: "token-a", Status: ReplyOutboxManualReview,
	})
	require.NoError(t, err)
	require.True(t, settled)

	_, err = pool.Exec(ctx, `
		UPDATE bot_webhook_inbox SET updated_at = clock_timestamp() - interval '9 days';
		UPDATE bot_command_executions SET updated_at = clock_timestamp() - interval '9 days';
		UPDATE bot_reply_outbox SET updated_at = clock_timestamp() - interval '9 days';
	`)
	require.NoError(t, err)
	claimed, err = commands.Claim(ctx, "message:retained-command", "webhook", "retained-token")
	require.NoError(t, err)
	require.True(t, claimed)
	completed, err = commands.Complete(ctx, "message:retained-command", "retained-token", CommandExecutionSucceeded)
	require.NoError(t, err)
	require.True(t, completed)
	var completedUpdatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at FROM bot_command_executions WHERE message_id = 'message:retained-command'`).Scan(&completedUpdatedAt))

	result, err := ledger.Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.DeletedInbox)
	assert.Equal(t, int64(1), result.DeletedCommand)
	assert.Equal(t, int64(1), result.DeletedOutbox)
	var retainedUpdatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at FROM bot_command_executions WHERE message_id = 'message:retained-command'`).Scan(&retainedUpdatedAt))
	assert.Equal(t, completedUpdatedAt, retainedUpdatedAt, "maintenance must not rewrite terminal commands while pruning")
	var manualCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox WHERE status = 'manual_review'`).Scan(&manualCount))
	assert.Equal(t, 1, manualCount, "manual review keeps a longer operator decision window")

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox SET updated_at = clock_timestamp() - interval '31 days' WHERE status = 'manual_review'`)
	require.NoError(t, err)
	result, err = ledger.Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.DeletedOutbox)
}
