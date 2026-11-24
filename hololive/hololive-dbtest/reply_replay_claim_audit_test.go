package dbtest

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const replayClaimSQL = `UPDATE bot_reply_outbox SET status = 'submitting', claim_token = 'claim-token',
	lease_until = now() + interval '1 minute', first_attempt_at = now(), attempts = attempts + 1 WHERE id = $1`

func TestReplayClaimAuditAppendsOnlyOnReplayedClaim(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	insertOutbox := func(key string, grants int) int64 {
		var id int64

		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO bot_reply_outbox
			(message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id, status, operator_replay_grants)
			VALUES ('message:' || $1, 'final', 0, 'room-1', '{"kind":"text","message":"x"}', repeat('a', 64), $1, 'manual_review', $2)
			RETURNING id`, key, grants).Scan(&id))

		return id
	}

	replayed := insertOutbox("replay-audit-granted", 0)

	var outcome string

	require.NoError(t, pool.QueryRow(ctx, `SELECT grant_bot_reply_outbox_manual_replay($1, 'operator', 'resend')`, replayed).Scan(&outcome))
	require.Equal(t, "replayed", outcome)

	_, err := pool.Exec(ctx, replayClaimSQL, replayed)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox SET lease_until = now() + interval '2 minutes' WHERE id = $1`, replayed)
	require.NoError(t, err)

	var events int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = $1 AND grant_number = 1 AND event_type = 'replayed' AND actor = 'operator' AND reason = 'resend'`, replayed).Scan(&events))
	require.Equal(t, 1, events)

	ungranted := insertOutbox("replay-audit-missing", 1)

	_, err = pool.Exec(ctx, replayClaimSQL, ungranted)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "claim without grant audit error = %v", err)
	require.Equal(t, "23514", pgErr.Code)

	fresh := insertOutbox("replay-audit-first-try", 0)

	_, err = pool.Exec(ctx, replayClaimSQL, fresh)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1`, fresh).Scan(&events))
	require.Equal(t, 0, events)
}
