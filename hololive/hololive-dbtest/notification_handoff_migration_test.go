package dbtest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const notificationHandoffMigration = "330_notification_delivery_handoff_check.sql"

// notification handoff shape CHECK는 확인 대기 SENT 행, 확인을 끝낸 행, handoff 없는 행을 받는다. 빈 requestId,
// requestId 없는 확인 예정·완료, 끝난 확인의 예정 시각, SENT가 아닌 행의 예정 시각, 예정 시각 없는 확인 대기 SENT
// 행은 거부한다. 재적용은 제약을 바꾸지 않는다.
func TestNotificationHandoffShape(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	cases := []struct {
		name, set string
		accepted  bool
	}{
		{name: "sent without handoff", accepted: true, set: `status = 'SENT'`},
		{name: "awaiting handoff check", accepted: true, set: `status = 'SENT', handoff_request_id = 'iris-1', handoff_next_check_at = now()`},
		{name: "handoff checked", accepted: true, set: `status = 'SENT', handoff_request_id = 'iris-1', handoff_checked_at = now()`},
		{name: "quarantined after check", accepted: true, set: `status = 'QUARANTINED', handoff_request_id = 'iris-1', handoff_checked_at = now()`},
		{name: "empty request id", set: `status = 'SENT', handoff_request_id = '', handoff_checked_at = now()`},
		{name: "next check without request id", set: `status = 'SENT', handoff_next_check_at = now()`},
		{name: "checked without request id", set: `status = 'SENT', handoff_checked_at = now()`},
		{name: "next check after checked", set: `status = 'SENT', handoff_request_id = 'iris-1', handoff_next_check_at = now(), handoff_checked_at = now()`},
		{name: "next check on pending row", set: `handoff_request_id = 'iris-1', handoff_next_check_at = now()`},
		{name: "unconfirmed sent without next check", set: `status = 'SENT', handoff_request_id = 'iris-1'`},
	}

	for application, attempt := range []string{"applied", "reapplied"} {
		if application > 0 {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, notificationHandoffMigration))
		}

		for i, tc := range cases {
			periodKey := fmt.Sprintf("%s-%d", attempt, i)

			var id int64

			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO notification_delivery_outbox (kind, period_key, room_id, content_id, payload)
				VALUES ('MEMBER_NEWS_WEEKLY', $1::text, 'room-1', $1::text || ':room-1', '{"message":"m"}'::jsonb)
				RETURNING id`, periodKey).Scan(&id))

			_, err = pool.Exec(ctx, `UPDATE notification_delivery_outbox SET `+tc.set+` WHERE id = $1`, id)
			if tc.accepted {
				require.NoError(t, err, "%s: %s", attempt, tc.name)

				continue
			}

			requireSQLState(t, err, sqlStateCheckViolation)
		}

		var definitions []string

		require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(conname || ':' || convalidated::text ORDER BY conname)
			FROM pg_constraint
			WHERE conrelid = 'public.notification_delivery_outbox'::regclass
			  AND conname LIKE 'chk_notification_delivery_outbox_handoff_shape%'`).
			Scan(&definitions))
		require.Equal(t, []string{"chk_notification_delivery_outbox_handoff_shape:true"}, definitions, attempt)
	}
}
