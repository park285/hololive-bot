package telemetry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
)

func seedLoggedTelemetryRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int, eventAt time.Time) {
	t.Helper()

	for i := range count {
		_, err := pool.Exec(ctx, `
			INSERT INTO youtube_notification_delivery_telemetry
				(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type, dedupe_key, delivery_mode, send_result, event_at, next_attempt_at, created_at, logged_at)
			VALUES ($1, 1, 1, 'UC_ret', $2, $2, 'room-ret', 'LIVE', $3, 'direct', 'sent', $4, $4, $4, $4)
		`, int64(1000+i), fmt.Sprintf("content-ret-%d", i), fmt.Sprintf("dedupe-ret-%d", i), eventAt)
		require.NoError(t, err)
	}
}

func countTelemetryRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()

	var count int64
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM youtube_notification_delivery_telemetry").Scan(&count))
	return count
}

func TestDeleteLoggedBefore_DrainsAllEligibleRowsInBatches(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewRepository(pool)
	oldEventAt := time.Now().UTC().Add(-100 * 24 * time.Hour)
	seedLoggedTelemetryRows(t, ctx, pool, 3, oldEventAt)

	deleted, err := repository.deleteLoggedBeforeInBatches(ctx, time.Now().UTC().Add(-90*24*time.Hour), 1)

	require.NoError(t, err)
	require.Equal(t, int64(3), deleted, "배치 크기보다 많은 대상도 루프로 전량 삭제해야 한다")
	require.Zero(t, countTelemetryRows(t, ctx, pool))
}

func TestDeleteLoggedBefore_KeepsUnloggedAndFreshRows(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewRepository(pool)
	oldEventAt := time.Now().UTC().Add(-100 * 24 * time.Hour)
	seedLoggedTelemetryRows(t, ctx, pool, 1, oldEventAt)
	seedLoggedTelemetryRows(t, ctx, pool, 0, oldEventAt)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_notification_delivery_telemetry
			(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type, dedupe_key, delivery_mode, send_result, event_at, next_attempt_at, created_at, logged_at)
		VALUES (2000, 1, 1, 'UC_ret', 'content-unlogged', 'content-unlogged', 'room-ret', 'LIVE', 'dedupe-unlogged', 'direct', 'sent', $1, $1, $1, NULL)
	`, oldEventAt)
	require.NoError(t, err)

	freshEventAt := time.Now().UTC().Add(-time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_notification_delivery_telemetry
			(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type, dedupe_key, delivery_mode, send_result, event_at, next_attempt_at, created_at, logged_at)
		VALUES (2001, 1, 1, 'UC_ret', 'content-fresh', 'content-fresh', 'room-ret', 'LIVE', 'dedupe-fresh', 'direct', 'sent', $1, $1, $1, $1)
	`, freshEventAt)
	require.NoError(t, err)

	deleted, err := repository.DeleteLoggedBefore(ctx, time.Now().UTC().Add(-90*24*time.Hour))

	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Equal(t, int64(2), countTelemetryRows(t, ctx, pool))
}
