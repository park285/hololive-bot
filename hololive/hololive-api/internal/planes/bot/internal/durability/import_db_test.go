package durability

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	importOperator  = "operator@example.com"
	importMessageID = "message:import"
)

func importFixture(now int64) (map[ImportTable][][]any, ImportSummary) {
	hash := strings.Repeat("a", 64)
	rows := map[ImportTable][][]any{
		ImportWebhookInbox: {
			{int64(10), importMessageID, "room-1", "room:import", `{"text":"hello"}`, "pending", int64(0), nil, nil, now, "", nil, "", now, now},
		},
		ImportWebhookHeads: {
			{"room:import", importMessageID, now},
		},
		ImportCommandExecutions: {
			{int64(20), importMessageID, "", "claimed", "claim-token", now, nil, now, now},
		},
		ImportReplyOutbox: {
			{
				int64(30), importMessageID, "reply", int64(0), "room-1", `{"kind":"text"}`, hash, "client-request-30", "pending",
				int64(0), nil, "", nil, nil, "", now, now, now, int64(0), nil, nil,
			},
			{
				int64(31), importMessageID, "reply", int64(1), "room-1", nil, hash, "client-request-31", "discarded",
				int64(5), now - time.Hour.Milliseconds(), "", nil, nil, "operator discarded manual review without replay", now, now, now, int64(0), nil, nil,
			},
			{
				int64(32), importMessageID, "reply", int64(2), "room-1", `{"kind":"text"}`, hash, "client-request-32", "retryable_pre_dispatch",
				int64(6), now - time.Hour.Milliseconds(), "", nil, nil, "", now, now, now, int64(1), nil, nil,
			},
		},
		ImportReplyReplayAudit: {
			{int64(40), int64(32), int64(1), "granted", importOperator, "incident replay", now},
			{int64(41), int64(32), int64(1), "replayed", importOperator, "incident replay", now},
		},
		ImportReplyResolutionAudit: {
			{int64(50), int64(31), "discarded_without_replay", "not_found", importOperator, "incident discard", now},
		},
		ImportWebhookNonces: {
			{[]byte(strings.Repeat("n", 32)), now + time.Minute.Milliseconds()},
		},
	}

	summary := ImportSummary{}
	summary.Add("bot_webhook_inbox", "pending", 1)
	summary.Add("bot_webhook_heads", "", 1)
	summary.Add("bot_command_executions", "claimed", 1)
	summary.Add("bot_reply_outbox", "pending", 1)
	summary.Add("bot_reply_outbox", "discarded", 1)
	summary.Add("bot_reply_outbox", "retryable_pre_dispatch", 1)
	summary.Add("bot_reply_outbox_replay_audit", "granted", 1)
	summary.Add("bot_reply_outbox_replay_audit", "replayed", 1)
	summary.Add("bot_reply_outbox_resolution_audit", "", 1)
	summary.Add("bot_webhook_nonces", "", 1)

	return rows, summary
}

func TestImportRestoresDiscardedRepliesAndContinuesIdentifiers(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	rows, summary := importFixture(time.Now().UnixMilli())

	require.NoError(t, store.Import(ctx, rows, summary))

	var (
		status      string
		payloadNull bool
		rowVersion  int64
	)

	require.NoError(t, store.QueryRow(ctx, "SELECT status, payload IS NULL, row_version FROM bot_reply_outbox WHERE id = 31").
		Scan(&status, &payloadNull, &rowVersion))
	require.Equal(t, ReplyOutboxDiscarded, status)
	require.True(t, payloadNull, "discarded rows keep their scrubbed payload")
	require.Equal(t, int64(1), rowVersion)

	_, err := newTestReplyOutboxRepository(store).Insert(ctx, newReplyOutboxEntry("message:after-import", 0, `{"body":"next"}`))
	require.NoError(t, err)

	var nextID int64

	require.NoError(t, store.QueryRow(ctx, "SELECT id FROM bot_reply_outbox WHERE message_id = 'message:after-import'").Scan(&nextID))
	require.Greater(t, nextID, int64(32), "new rows continue after the imported identifiers")

	err = store.Import(ctx, rows, summary)
	require.ErrorIs(t, err, ErrImportTargetNotEmpty)
}

func TestImportRollsBackWhenTheSummaryDisagrees(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	rows, summary := importFixture(time.Now().UnixMilli())
	summary.Add("bot_reply_outbox", "pending", 2)

	err := store.Import(ctx, rows, summary)
	require.ErrorIs(t, err, ErrImportSummaryMismatch)

	var outboxRows, auditRows int

	require.NoError(t, store.QueryRow(ctx, "SELECT count(*) FROM bot_reply_outbox").Scan(&outboxRows))
	require.NoError(t, store.QueryRow(ctx, "SELECT count(*) FROM bot_reply_outbox_resolution_audit").Scan(&auditRows))
	require.Zero(t, outboxRows+auditRows, "a rejected import leaves the target empty")
}

func TestImportRequiresEveryTable(t *testing.T) {
	store := newTestStore(t)
	rows, summary := importFixture(time.Now().UnixMilli())
	delete(rows, ImportWebhookNonces)

	require.ErrorIs(t, store.Import(t.Context(), rows, summary), ErrInvalidArgument)
}
