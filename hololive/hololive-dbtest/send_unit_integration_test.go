package dbtest

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// delivery는 상태와 무관하게 send unit 없이 기록할 수 없습니다(migration 301 NOT NULL).
func TestAlarmDispatchDeliveriesRequireSendUnit(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		) VALUES ($1, repeat('a', 64), 'LIVE', 'unitless-channel', 'unitless-stream', 'unitless', '{}'::jsonb)
		RETURNING id`, "unitless-event-"+fmt.Sprint(time.Now().UnixNano())).Scan(&eventID))

	for _, status := range []string{"pending", "dlq", "sent"} {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, next_attempt_at, dlq_at, sent_at)
			VALUES ($1, 'room-unitless', $2, $3, NOW(), NOW(), NOW())`,
			eventID, "unitless-"+status, status)

		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		require.True(t, ok, "%s: want not-null violation, got %v", status, err)
		require.Equal(t, "23502", pgErr.Code, status)
		require.Equal(t, "send_unit_id", pgErr.ColumnName, status)
	}
}

// migration 226이 비교 전용 shadowed 상태를 CHECK에서 제거했습니다.
func TestAlarmDispatchDeliveryStatusRejectsShadowed(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
        INSERT INTO alarm_dispatch_events(event_key,payload_hash,alarm_type,channel_id,stream_id,category,payload)
        VALUES('status-check',repeat('a',64),'LIVE','channel-status','stream-status','status','{}'::jsonb)
        RETURNING id`).Scan(&eventID))

	var unitID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
        INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id)
        VALUES(repeat('c',64),'status-group','room-status','status-check-request')
        RETURNING id`).Scan(&unitID))

	_, err := pool.Exec(t.Context(), `INSERT INTO alarm_dispatch_deliveries(event_id,room_id,dedupe_key,dispatch_group_key,send_unit_id,status,sent_at)
        VALUES($1,'room-status','status-check','status-group',$2,'sent',now())`, eventID, unitID)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET status = 'shadowed'`)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "want status check violation, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "alarm_dispatch_deliveries_status_check", pgErr.ConstraintName)
}
