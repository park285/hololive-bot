package dbtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	botReplyOutboxUnleasedAcceptedMigration = "326_bot_reply_outbox_unleased_accepted.sql"
	botReplyOutboxAcceptedStrictMigration   = "332_bot_reply_outbox_accepted_strict.sql"
	sqlStateObjectNotInPrerequisiteState    = "55000"
	leasedAcceptedShape                     = `status = 'accepted', iris_request_id = 'iris-1', claim_token = 'owner', lease_until = now(), first_attempt_at = now()`
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

// 엄격 shape CHECK는 lease 없이 handoff 확인을 기다리는 accepted만 받고, 이전 바이너리의 leased accepted와 그 밖의
// 잘못된 모양은 거부한다. 326·332 재적용은 제약을 바꾸지 않는다.
func TestBotReplyOutboxAcceptedShape(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

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

	for application, attempt := range []string{"applied", "reapplied"} {
		if application > 0 {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, botReplyOutboxUnleasedAcceptedMigration))
			require.NoError(t, applyMigrationFile(ctx, pool, dir, botReplyOutboxAcceptedStrictMigration))
		}

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

// 332는 leased accepted가 남아 있으면 완화 CHECK를 그대로 둔 채 중단하고, 그 행을 정리한 뒤에는 엄격 CHECK로 바꾼다.
func TestBotReplyOutboxAcceptedStrictRequiresNoLeasedAccepted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ALTER TABLE public.bot_reply_outbox DROP CONSTRAINT chk_bot_reply_outbox_state_shape`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, botReplyOutboxUnleasedAcceptedMigration))
	require.NoError(t, insertBotReplyOutboxShape(ctx, t, pool, "message:legacy-leased", leasedAcceptedShape))

	err = applyMigrationFile(ctx, pool, dir, botReplyOutboxAcceptedStrictMigration)
	requireSQLState(t, err, sqlStateObjectNotInPrerequisiteState)
	require.NoError(t, insertBotReplyOutboxShape(ctx, t, pool, "message:legacy-still-writable", leasedAcceptedShape),
		"중단된 332는 완화 CHECK를 바꾸지 않는다")

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review', claim_token = NULL, lease_until = NULL
		WHERE status = 'accepted' AND claim_token IS NOT NULL`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, botReplyOutboxAcceptedStrictMigration))

	requireSQLState(t, insertBotReplyOutboxShape(ctx, t, pool, "message:legacy-after-strict", leasedAcceptedShape), sqlStateCheckViolation)
}
