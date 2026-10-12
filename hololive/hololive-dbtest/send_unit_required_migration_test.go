package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const sendUnitRequiredMigration = "301_alarm_dispatch_deliveries_require_send_unit.sql"

// 301 적용 전 운영 형태: send_unit_id는 NULL을 허용하고 224 CHECK가 활성 상태의 NULL만 거부한다.
const sendUnitOptionalShapeSQL = `
ALTER TABLE alarm_dispatch_deliveries ALTER COLUMN send_unit_id DROP NOT NULL;
ALTER TABLE alarm_dispatch_deliveries ADD CONSTRAINT alarm_dispatch_deliveries_active_send_unit_check
	CHECK (send_unit_id IS NOT NULL OR status <> ALL (ARRAY['pending', 'retry', 'leased', 'sending']));
`

// 처리되지 않은 dlq legacy 행이 있으면 아무것도 지우지 않고 실패한다. 종료된 legacy 행만 남으면 그 행만 지우고
// send_unit_id를 NOT NULL로 바꾸며, 관리자 작업 기록은 남기고 재적용은 아무것도 바꾸지 않는다.
func TestSendUnitRequiredMigrationDeletesSettledLegacyRowsOnly(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	//nolint:misspell // alarm_dispatch_deliveries.cancelled_at은 스키마 열 이름이라 canceled로 바꾸면 SQL이 깨진다.
	_, err = pool.Exec(ctx, sendUnitOptionalShapeSQL+`
		INSERT INTO alarm_dispatch_events (event_key, payload_hash, alarm_type, payload)
		VALUES ('legacy-event', repeat('a', 64), 'LIVE', '{}');
		INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
		VALUES (repeat('b', 64), 'kept-group', 'room-kept', 'kept-request-0001');
		INSERT INTO alarm_dispatch_deliveries (id, event_id, room_id, dedupe_key, dispatch_group_key, send_unit_id, status, sent_at, cancelled_at, dlq_at)
		SELECT v.id, e.id, 'room-legacy', v.dedupe_key, v.group_key, u.id, v.status, v.sent_at, v.cancelled_at, v.dlq_at
		FROM (VALUES
			(9001, 'legacy-sent', NULL, 'sent', now(), NULL::timestamptz, NULL::timestamptz),
			(9002, 'legacy-cancelled', NULL, 'cancelled', NULL, now(), NULL),
			(9003, 'legacy-dlq', NULL, 'dlq', NULL, NULL, now()),
			(9004, 'kept-sent', 'kept-group', 'sent', now(), NULL, NULL)
		) AS v(id, dedupe_key, group_key, status, sent_at, cancelled_at, dlq_at)
		CROSS JOIN alarm_dispatch_events e
		LEFT JOIN alarm_dispatch_send_units u ON v.group_key IS NOT NULL;
		INSERT INTO alarm_dispatch_admin_actions (delivery_id, action, operator_id, reason, from_status, to_status, duplicate_risk_ack)
		VALUES (9001, 'manual_cancel', 'operator', 'legacy audit', 'dlq', 'sent', false);`)
	require.NoError(t, err)

	err = applyMigrationFile(ctx, pool, dir, sendUnitRequiredMigration)
	requireSQLState(t, err, sqlStateRaiseException)
	require.ErrorContains(t, err, "alarm_dispatch_deliveries has unsettled rows without send unit; rows kept")
	require.Equal(t, []int64{9001, 9002, 9003, 9004}, deliveryIDs(t, pool))

	_, err = pool.Exec(ctx, `DELETE FROM alarm_dispatch_deliveries WHERE id = 9003`)
	require.NoError(t, err)

	for _, attempt := range []string{"first", "reapply"} {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, sendUnitRequiredMigration), attempt)
		require.Equal(t, []int64{9004}, deliveryIDs(t, pool), attempt)

		var notNull, activeCheckExists, auditKept bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT
			(SELECT attnotnull FROM pg_attribute
			 WHERE attrelid = 'public.alarm_dispatch_deliveries'::regclass AND attname = 'send_unit_id'),
			EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'alarm_dispatch_deliveries_active_send_unit_check'),
			EXISTS (SELECT 1 FROM alarm_dispatch_admin_actions WHERE reason = 'legacy audit' AND delivery_id IS NULL)`).
			Scan(&notNull, &activeCheckExists, &auditKept))
		require.True(t, notNull, attempt)
		require.False(t, activeCheckExists, attempt)
		require.True(t, auditKept, attempt)
	}
}

func deliveryIDs(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()

	var ids []int64

	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COALESCE(array_agg(id ORDER BY id), '{}') FROM alarm_dispatch_deliveries`).Scan(&ids))

	return ids
}
