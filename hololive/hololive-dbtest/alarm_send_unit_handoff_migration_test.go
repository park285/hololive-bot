package dbtest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const alarmSendUnitHandoffMigration = "329_alarm_send_unit_handoff_check.sql"

// handoff shape CHECK는 고정 요청이 있는 단위의 확인 대기·확인 완료와 handoff가 없는 단위를 받고, 고정 요청 없는
// requestId, requestId 없는 확인 예정·완료, 끝난 확인의 예정 시각, 빈 requestId는 거부한다. 재적용은 제약을 바꾸지
// 않는다.
func TestAlarmSendUnitHandoffShape(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const pinned = `request_body = 'body', request_route = 'text', request_body_hash = repeat('a', 64),
		request_delivery_ids = ARRAY[1]::BIGINT[], base_client_request_id = client_request_id`

	cases := []struct {
		name, set string
		accepted  bool
	}{
		{name: "unpinned without handoff", accepted: true, set: `request_generation = 0`},
		{name: "awaiting handoff check", accepted: true, set: pinned + `, handoff_request_id = 'iris-1', handoff_next_check_at = now()`},
		{name: "handoff checked", accepted: true, set: pinned + `, handoff_request_id = 'iris-1', handoff_checked_at = now()`},
		{name: "request id without pinned request", set: `handoff_request_id = 'iris-1'`},
		{name: "empty request id", set: pinned + `, handoff_request_id = ''`},
		{name: "next check without request id", set: pinned + `, handoff_next_check_at = now()`},
		{name: "checked without request id", set: pinned + `, handoff_checked_at = now()`},
		{name: "next check after checked", set: pinned + `, handoff_request_id = 'iris-1', handoff_next_check_at = now(), handoff_checked_at = now()`},
	}

	for _, attempt := range []string{"applied", "reapplied"} {
		if attempt == "reapplied" {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, alarmSendUnitHandoffMigration))
		}

		for i, tc := range cases {
			clientRequestID := fmt.Sprintf("hololive-alarm:%s-%d", attempt, i)

			var unit int64

			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
				VALUES (encode(sha256(convert_to($1, 'UTF8')), 'hex'), $1, 'room-1', $1)
				RETURNING id`, clientRequestID).Scan(&unit))

			_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET `+tc.set+` WHERE id = $1`, unit)
			if tc.accepted {
				require.NoError(t, err, "%s: %s", attempt, tc.name)

				continue
			}

			requireSQLState(t, err, sqlStateCheckViolation)
		}

		var definitions []string

		require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(conname || ':' || convalidated::text ORDER BY conname)
			FROM pg_constraint
			WHERE conrelid = 'public.alarm_dispatch_send_units'::regclass AND conname LIKE 'chk_alarm_send_unit_handoff_shape%'`).
			Scan(&definitions))
		require.Equal(t, []string{"chk_alarm_send_unit_handoff_shape:true"}, definitions, attempt)
	}
}
