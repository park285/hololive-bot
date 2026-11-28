package durability

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurableTerminalPayloadScrubAndRetention(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	seedTerminalInboxAndCommand(ctx, t, store)
	seedTerminalAndManualReviewOutbox(ctx, t, store)
	ageDurableLedgerRows(ctx, t, store)

	retainedUpdatedAt := seedRetainedTerminalCommand(ctx, t, store)

	result, err := NewDurableLedgerRepository(store).Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.DeletedInbox)
	assert.Equal(t, int64(1), result.DeletedCommand)
	assert.Equal(t, int64(1), result.DeletedOutbox)

	assertRetainedCommandIsUntouched(ctx, t, store, retainedUpdatedAt)
	assertManualReviewOutlivesTerminalRetention(ctx, t, store)
}

func seedTerminalInboxAndCommand(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	inbox := NewInboxRepository(store)
	commands := NewCommandExecutionRepository(store)

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

	require.NoError(t, store.QueryRow(ctx, `SELECT payload FROM bot_webhook_inbox WHERE message_id = ?1`, claim.MessageID).Scan(&payload))
	assert.Equal(t, `{}`, payload)
}

func seedTerminalAndManualReviewOutbox(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	outbox := newTestReplyOutboxRepository(store)

	entry := newReplyOutboxEntry("message:retention-reply", 0, `{"kind":"text","message":"answer"}`)
	claimReply := claimOne(ctx, t, outbox, entry)
	completeReplyHandoff(ctx, t, outbox, claimReply)

	manual := newReplyOutboxEntry("message:retention-manual", 0, `{"kind":"text","message":"review"}`)
	manualClaim := claimOne(ctx, t, outbox, manual)

	settled, err := outbox.Settle(ctx, ReplyOutboxSettlement{
		ID: manualClaim.ID, ClaimToken: testClaimToken, Status: ReplyOutboxManualReview,
	})
	require.NoError(t, err)
	require.True(t, settled)
}

func ageDurableLedgerRows(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	aged := time.Now().Add(-9 * 24 * time.Hour).UnixMilli()

	for _, statement := range []string{
		`UPDATE bot_webhook_inbox SET updated_at = ?1`,
		`UPDATE bot_command_executions SET updated_at = ?1`,
		`UPDATE bot_reply_outbox SET row_version = row_version + 1, updated_at = ?1`,
	} {
		_, err := store.Exec(ctx, statement, aged)
		require.NoError(t, err)
	}
}

func seedRetainedTerminalCommand(ctx context.Context, t *testing.T, store *Store) int64 {
	t.Helper()

	commands := NewCommandExecutionRepository(store)

	claimed, err := commands.Claim(ctx, "message:retained-command", "webhook", "retained-token")
	require.NoError(t, err)
	require.True(t, claimed)

	completed, err := commands.Complete(ctx, "message:retained-command", "retained-token", CommandExecutionSucceeded)
	require.NoError(t, err)
	require.True(t, completed)

	var completedUpdatedAt int64

	require.NoError(t, store.QueryRow(ctx,
		`SELECT updated_at FROM bot_command_executions WHERE message_id = 'message:retained-command'`).Scan(&completedUpdatedAt))

	return completedUpdatedAt
}

func assertRetainedCommandIsUntouched(ctx context.Context, t *testing.T, store *Store, completedUpdatedAt int64) {
	t.Helper()

	var retainedUpdatedAt int64

	require.NoError(t, store.QueryRow(ctx,
		`SELECT updated_at FROM bot_command_executions WHERE message_id = 'message:retained-command'`).Scan(&retainedUpdatedAt))
	assert.Equal(t, completedUpdatedAt, retainedUpdatedAt, "maintenance must not rewrite terminal commands while pruning")
}

func assertManualReviewOutlivesTerminalRetention(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	var manualCount int

	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox WHERE status = 'manual_review'`).Scan(&manualCount))
	assert.Equal(t, 1, manualCount, "manual review keeps a longer operator decision window")

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version = row_version + 1, updated_at = ?1 WHERE status = 'manual_review'`,
		time.Now().Add(-31*24*time.Hour).UnixMilli())
	require.NoError(t, err)

	result, err := NewDurableLedgerRepository(store).Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.DeletedOutbox)
}

func TestDurablePruneOutboxDeletesOldestEligibleRowsAcrossStatuses(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	now := time.Now()
	daysAgo := func(days int) int64 { return now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli() }

	for i, row := range []struct {
		messageID, status string
		updatedAt         int64
	}{
		{messageID: "terminal-40d", status: "handoff_completed", updatedAt: daysAgo(40)},
		{messageID: "review-35d", status: "manual_review", updatedAt: daysAgo(35)},
		{messageID: "terminal-35d", status: "dead", updatedAt: daysAgo(35)},
		{messageID: "review-35d-later-id", status: "manual_review", updatedAt: daysAgo(35)},
		{messageID: "terminal-9d", status: "dead", updatedAt: daysAgo(9)},
		{messageID: "review-20d", status: "manual_review", updatedAt: daysAgo(20)},
		{messageID: "terminal-1d", status: "permanent_conflict", updatedAt: daysAgo(1)},
		{messageID: "pending-40d", status: "pending", updatedAt: daysAgo(40)},
	} {
		_, err := store.Exec(ctx, `INSERT INTO bot_reply_outbox (message_id, phase, ordinal, room_id, payload, payload_hash,
			client_request_id, status, available_at, created_at, updated_at)
			VALUES (?1, 'final', 0, 'room-1', '{"kind":"text","message":"body"}', printf('%.64c', 'a'),
			printf('prune-order-%04d', ?2), ?3, ?4, ?4, ?4)`, row.messageID, i, row.status, row.updatedAt)
		require.NoError(t, err)
	}

	repository := NewDurableLedgerRepository(store)

	for _, want := range [][]string{
		{"review-35d", "terminal-40d"},
		{"review-35d-later-id", "terminal-35d"},
		{"terminal-9d"},
		{},
	} {
		before := remainingOutboxMessages(ctx, t, store)

		result, err := repository.Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(len(want)), result.DeletedOutbox)

		after := remainingOutboxMessages(ctx, t, store)
		deleted := make([]string, 0, len(want))

		for _, messageID := range before {
			if !slices.Contains(after, messageID) {
				deleted = append(deleted, messageID)
			}
		}

		assert.ElementsMatch(t, want, deleted)
	}

	assert.Equal(t, []string{"pending-40d", "review-20d", "terminal-1d"}, remainingOutboxMessages(ctx, t, store))
}

func remainingOutboxMessages(ctx context.Context, t *testing.T, store *Store) []string {
	t.Helper()

	rows, err := store.Query(ctx, `SELECT message_id FROM bot_reply_outbox ORDER BY message_id`)
	require.NoError(t, err)

	defer rows.Close()

	var messageIDs []string

	for rows.Next() {
		var messageID string

		require.NoError(t, rows.Scan(&messageID))

		messageIDs = append(messageIDs, messageID)
	}

	require.NoError(t, rows.Err())

	return messageIDs
}
