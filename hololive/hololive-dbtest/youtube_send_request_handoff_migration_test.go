package dbtest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const youtubeSendRequestHandoffMigration = "336_youtube_send_request_handoff_check.sql"

// youtube 고정 요청 handoff shape CHECK는 확인 대기·확인 완료 요청과 handoff가 없는 요청을 받고, 빈 requestId,
// requestId 없는 확인 예정·완료, 끝난 확인의 예정 시각, 예정 시각도 확인 시각도 없는 requestId는 거부한다. 재적용은
// 제약을 바꾸지 않는다.
func TestYouTubeSendRequestHandoffShape(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	cases := []struct {
		name, set string
		accepted  bool
	}{
		{name: "without handoff", accepted: true, set: `generation = 0`},
		{name: "awaiting handoff check", accepted: true, set: `handoff_request_id = 'iris-1', handoff_next_check_at = now()`},
		{name: "handoff checked", accepted: true, set: `handoff_request_id = 'iris-1', handoff_checked_at = now()`},
		{name: "empty request id", set: `handoff_request_id = '', handoff_next_check_at = now()`},
		{name: "request id without check state", set: `handoff_request_id = 'iris-1'`},
		{name: "next check without request id", set: `handoff_next_check_at = now()`},
		{name: "checked without request id", set: `handoff_checked_at = now()`},
		{name: "next check after checked", set: `handoff_request_id = 'iris-1', handoff_next_check_at = now(), handoff_checked_at = now()`},
	}

	for index, attempt := range []string{"initial", "reapplication"} {
		if index > 0 {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, youtubeSendRequestHandoffMigration))
		}

		for i, tc := range cases {
			baseID := fmt.Sprintf("youtube:%s-%d", attempt, i)

			_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_send_request
				(base_id, room_id, message, message_hash, route, dedupe_keys, member_ids)
				VALUES ($1, 'room-1', 'body', repeat('a', 64), 'text', ARRAY[$1], ARRAY[1]::BIGINT[])`, baseID)
			require.NoError(t, err)

			_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET `+tc.set+` WHERE base_id = $1`, baseID)
			if tc.accepted {
				require.NoError(t, err, "%s: %s", attempt, tc.name)

				continue
			}

			requireSQLState(t, err, sqlStateCheckViolation)
		}

		var definitions []string

		require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(conname || ':' || convalidated::text ORDER BY conname)
			FROM pg_constraint
			WHERE conrelid = 'public.youtube_notification_send_request'::regclass
			  AND conname LIKE 'chk_youtube_send_request_handoff_shape%'`).
			Scan(&definitions))
		require.Equal(t, []string{"chk_youtube_send_request_handoff_shape:true"}, definitions, attempt)
	}
}
