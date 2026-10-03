package dbtest

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// 활성 delivery는 저장된 send unit 없이 기록할 수 없으며 종단 행을 활성 상태로 되돌릴 때도 CHECK가 거절해야 합니다.
func TestAlarmDispatchDeliveriesRequirePersistedSendUnitWhileActive(t *testing.T) {
	pool := NewPool(t)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		) VALUES ($1, repeat('a', 64), 'LIVE', 'legacy-channel', 'legacy-stream', 'legacy', '{}'::jsonb)
		RETURNING id`, "legacy-event-"+fmt.Sprint(time.Now().UnixNano())).Scan(&eventID))

	for _, status := range []string{"pending", "retry"} {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, next_attempt_at)
			VALUES ($1, 'room-legacy', $2, $3, NOW() - INTERVAL '1 minute')`,
			eventID, "legacy-active-"+status, status)
		requireActiveSendUnitViolation(t, err)
	}

	var terminalID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, dlq_at)
		VALUES ($1, 'room-legacy', 'legacy-terminal-dlq', 'dlq', NOW())
		RETURNING id`, eventID).Scan(&terminalID))

	_, err := pool.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries
		SET status = 'retry', next_attempt_at = NOW()
		WHERE id = $1`, terminalID)
	requireActiveSendUnitViolation(t, err)
}

func requireActiveSendUnitViolation(t *testing.T, err error) {
	t.Helper()

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "want active send unit check violation, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "alarm_dispatch_deliveries_active_send_unit_check", pgErr.ConstraintName)
}

// migration 226이 비교 전용 shadowed 상태를 CHECK에서 제거했습니다.
func TestAlarmDispatchDeliveryStatusRejectsShadowed(t *testing.T) {
	pool := NewPool(t)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
        INSERT INTO alarm_dispatch_events(event_key,payload_hash,alarm_type,channel_id,stream_id,category,payload)
        VALUES('status-check',repeat('a',64),'LIVE','channel-status','stream-status','status','{}'::jsonb)
        RETURNING id`).Scan(&eventID))

	_, err := pool.Exec(t.Context(), `INSERT INTO alarm_dispatch_deliveries(event_id,room_id,dedupe_key,status,sent_at)
        VALUES($1,'room-status','status-check','sent',now())`, eventID)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET status = 'shadowed'`)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "want status check violation, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "alarm_dispatch_deliveries_status_check", pgErr.ConstraintName)
}
