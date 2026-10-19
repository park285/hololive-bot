package dbtest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const botReplyOutboxUnleasedAcceptedMigration = "326_bot_reply_outbox_unleased_accepted.sql"

// 완화 shape CHECK는 이전 바이너리의 leased accepted와 새 바이너리의 unleased accepted를 함께 받고, 어느 쪽도
// 아닌 accepted와 accepted 밖의 handoff 확인 예정 시각은 거부한다. 재적용은 제약을 바꾸지 않는다.
func TestBotReplyOutboxUnleasedAcceptedShape(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	cases := []struct {
		name, set string
		accepted  bool
	}{
		{
			name: "legacy leased accepted", accepted: true,
			set: `status = 'accepted', iris_request_id = 'iris-1', claim_token = 'owner', lease_until = now(), first_attempt_at = now()`,
		},
		{
			name: "unleased accepted awaiting handoff", accepted: true,
			set: `status = 'accepted', iris_request_id = 'iris-1', first_attempt_at = now(), accepted_at = now(), handoff_next_check_at = now()`,
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

	for _, attempt := range []string{"applied", "reapplied"} {
		if attempt == "reapplied" {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, botReplyOutboxUnleasedAcceptedMigration))
		}

		for i, tc := range cases {
			messageID := fmt.Sprintf("message:%s-%d", attempt, i)
			_, err := pool.Exec(ctx, `INSERT INTO bot_reply_outbox (message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id)
				VALUES ($1, 'reply', 0, 'room-1', '{}'::jsonb, repeat('a', 64), $2)`,
				messageID, "hololive:v1:"+messageID+":reply:0")
			require.NoError(t, err)

			_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox SET `+tc.set+` WHERE message_id = $1`, messageID)
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
