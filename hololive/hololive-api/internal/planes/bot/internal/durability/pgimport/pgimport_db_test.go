package pgimport

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability/durabilitytest"
	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	pgOperator   = "operator@example.com"
	pgRoom       = "room-1"
	pgOrdering   = "room:pg-import"
	pgMessage    = "message:pg-import"
	pgReplyHash  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pgReplyPhase = "reply"
)

func TestCopyMovesEveryBotPlaneRowFromPostgres(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	discardedID, replayedID := seedPostgres(ctx, t, pool)
	target := durabilitytest.Open(t)

	summary, err := Copy(ctx, pool, target.Store)
	require.NoError(t, err)

	for bucket, want := range map[string]int64{
		"bot_webhook_inbox/pending":              1,
		"bot_webhook_inbox/succeeded":            1,
		"bot_webhook_heads/":                     1,
		"bot_command_executions/claimed":         1,
		"bot_command_executions/succeeded":       1,
		"bot_reply_outbox/pending":               1,
		"bot_reply_outbox/discarded":             1,
		"bot_reply_outbox/submitting":            1,
		"bot_reply_outbox_replay_audit/granted":  1,
		"bot_reply_outbox_replay_audit/replayed": 1,
		"bot_reply_outbox_resolution_audit/":     1,
		"bot_webhook_nonces/":                    1,
	} {
		require.Equal(t, want, summary[bucket], bucket)
	}

	var (
		status      string
		payloadNull bool
		state       string
	)

	require.NoError(t, target.DB.QueryRowContext(ctx, `SELECT outbox.status, outbox.payload IS NULL, audit.observed_iris_state
		FROM bot_reply_outbox AS outbox JOIN bot_reply_outbox_resolution_audit AS audit ON audit.outbox_id = outbox.id
		WHERE outbox.id = ?1`, discardedID).Scan(&status, &payloadNull, &state))
	require.Equal(t, durability.ReplyOutboxDiscarded, status)
	require.True(t, payloadNull)
	require.Equal(t, "not_found", state)

	var actor, claimToken string

	require.NoError(t, target.DB.QueryRowContext(ctx, `SELECT audit.actor, outbox.claim_token
		FROM bot_reply_outbox AS outbox JOIN bot_reply_outbox_replay_audit AS audit ON audit.outbox_id = outbox.id
		WHERE outbox.id = ?1 AND audit.event_type = 'replayed'`, replayedID).Scan(&actor, &claimToken))
	require.Equal(t, pgOperator, actor)
	require.Equal(t, "replay-claim", claimToken)

	var (
		payload   string
		createdAt int64
	)

	require.NoError(t, target.DB.QueryRowContext(ctx,
		"SELECT payload, created_at FROM bot_webhook_inbox WHERE message_id = ?1", pgMessage).Scan(&payload, &createdAt))
	require.JSONEq(t, `{"text":"hello"}`, payload)
	require.WithinDuration(t, time.Now(), time.UnixMilli(createdAt), time.Minute)

	digest := sha256.Sum256([]byte("nonce"))

	var nonces int

	require.NoError(t, target.DB.QueryRowContext(ctx, "SELECT count(*) FROM bot_webhook_nonces WHERE digest = ?1", digest[:]).Scan(&nonces))
	require.Equal(t, 1, nonces)

	_, err = Copy(ctx, pool, target.Store)
	require.ErrorIs(t, err, durability.ErrImportTargetNotEmpty)
}

func seedPostgres(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (discardedID, replayedID int64) {
	t.Helper()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pool.Exec(ctx, query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO public.bot_webhook_inbox (message_id, room_id, ordering_key, payload)
		VALUES ($1, $2, $3, '{"text":"hello"}')`, pgMessage, pgRoom, pgOrdering)
	exec(`INSERT INTO public.bot_webhook_inbox (message_id, room_id, ordering_key, payload, status, terminal_at)
		VALUES ('message:pg-done', $1, $2, '{}', 'succeeded', now())`, pgRoom, pgOrdering)
	exec(`INSERT INTO public.bot_webhook_heads (ordering_key, message_id) VALUES ($1, $2)`, pgOrdering, pgMessage)
	exec(`INSERT INTO public.bot_command_executions (message_id, claim_token) VALUES ($1, 'claim-1')`, pgMessage)
	exec(`INSERT INTO public.bot_command_executions (message_id, claim_token, status, result_summary, completed_at)
		VALUES ('message:pg-done', 'claim-2', 'succeeded', 'succeeded', now())`)
	exec(`INSERT INTO public.bot_webhook_nonces (digest, expires_at) VALUES (sha256('nonce'::bytea), now() + interval '10 minutes')`)

	insertReply := func(ordinal int, status string) int64 {
		t.Helper()

		var id int64

		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO public.bot_reply_outbox (
				message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id, status, attempts, first_attempt_at
			) VALUES ($1, $2, $3, $4, '{"kind":"text","message":"answer"}', $5, $6, $7, $8,
				CASE WHEN $8 > 0 THEN now() - interval '1 hour' END)
			RETURNING id`,
			pgMessage, pgReplyPhase, ordinal, pgRoom, pgReplyHash, fmt.Sprintf("client-request-pg-%d", ordinal), status, ordinal*5).Scan(&id))

		return id
	}

	insertReply(0, "pending")

	discardedID = insertReply(1, "manual_review")
	replayedID = insertReply(2, "manual_review")

	var outcome string

	require.NoError(t, pool.QueryRow(ctx, "SELECT public.discard_bot_reply_outbox_manual_review($1, $2, 'incident discard', 'not_found')",
		discardedID, pgOperator).Scan(&outcome))
	require.Equal(t, "discarded", outcome)
	require.NoError(t, pool.QueryRow(ctx, "SELECT public.grant_bot_reply_outbox_manual_replay($1, $2, 'incident replay')",
		replayedID, pgOperator).Scan(&outcome))
	require.Equal(t, "replayed", outcome)
	exec(`UPDATE public.bot_reply_outbox
		SET status = 'submitting', claim_token = 'replay-claim', lease_until = now() + interval '1 minute', attempts = attempts + 1
		WHERE id = $1`, replayedID)

	return discardedID, replayedID
}
