package dbtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	leasedAcceptedShape = `status = 'accepted', iris_request_id = 'iris-1', claim_token = 'owner', lease_until = now(), first_attempt_at = now()`
)

// insertBotReplyOutboxShape는 pending 행을 만든 뒤 주어진 열 값으로 갱신하고 그 갱신의 오류를 돌려준다.
func insertBotReplyOutboxShape(ctx context.Context, t *testing.T, pool *pgxpool.Pool, messageID, set string) error {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO bot_reply_outbox (message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id)
		VALUES ($1, 'reply', 0, 'room-1', '{}'::jsonb, repeat('a', 64), $2)`,
		messageID, "hololive:v1:"+messageID+":reply:0")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox SET `+set+` WHERE message_id = $1`, messageID)

	return err
}

// accepted는 lease 없이 handoff 확인을 기다리는 모양만 허용합니다.
func TestBotReplyOutboxAcceptedShape(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)

	cases := []struct {
		name, set string
		accepted  bool
	}{
		{
			name: "unleased accepted awaiting handoff", accepted: true,
			set: `status = 'accepted', iris_request_id = 'iris-1', first_attempt_at = now(), accepted_at = now(), handoff_next_check_at = now()`,
		},
		{name: "legacy leased accepted", set: leasedAcceptedShape},
		{
			name: "leased accepted with handoff schedule",
			set:  leasedAcceptedShape + `, accepted_at = now(), handoff_next_check_at = now()`,
		},
		{
			name: "accepted without lease or handoff schedule",
			set:  `status = 'accepted', iris_request_id = 'iris-1', first_attempt_at = now(), accepted_at = now()`,
		},
		{
			name: "accepted without iris request id",
			set:  `status = 'accepted', first_attempt_at = now(), accepted_at = now(), handoff_next_check_at = now()`,
		},
		{
			name: "handoff schedule outside accepted",
			set:  `status = 'retryable_pre_dispatch', handoff_next_check_at = now()`,
		},
		{
			name: "submitting without lease",
			set:  `status = 'submitting', first_attempt_at = now()`,
		},
	}

	for _, attempt := range []string{"baseline"} {

		for i, tc := range cases {
			err := insertBotReplyOutboxShape(ctx, t, pool, fmt.Sprintf("message:%s-%d", attempt, i), tc.set)
			if tc.accepted {
				require.NoError(t, err, "%s: %s", attempt, tc.name)

				continue
			}

			requireSQLState(t, err, sqlStateCheckViolation)
		}

		var definitions []string

		require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(conname || ':' || convalidated::text ORDER BY conname)
			FROM pg_constraint
			WHERE conrelid = 'public.bot_reply_outbox'::regclass AND conname LIKE 'chk_bot_reply_outbox_state_shape%'`).
			Scan(&definitions))
		require.Equal(t, []string{"chk_bot_reply_outbox_state_shape:true"}, definitions, attempt)
	}
}
