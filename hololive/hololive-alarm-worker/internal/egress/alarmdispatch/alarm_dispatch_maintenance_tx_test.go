package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestAlarmDispatchMaintenancePGWithAdvisoryLockRollsBackCallbackError(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}
	eventID := seedAlarmDispatchRetentionEvent(t, pool)
	callbackErr := errors.New("callback failed after delete")

	// 콜백이 같은 트랜잭션에서 지운 행은 콜백 오류와 함께 되돌아가야 한다.
	err := store.WithAdvisoryLock(t.Context(), 42, func(ctx context.Context, locked alarmDispatchMaintenanceDataStore) error {
		pgStore, ok := locked.(alarmDispatchMaintenancePgxStore)
		require.True(t, ok)

		tag, execErr := pgStore.db.Exec(ctx, "DELETE FROM alarm_dispatch_events WHERE id = $1", eventID)
		require.NoError(t, execErr)
		require.EqualValues(t, 1, tag.RowsAffected())

		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)

	var remaining int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_events WHERE id = $1", eventID).Scan(&remaining))
	require.Equal(t, 1, remaining)
}

func TestAlarmDispatchMaintenancePGWithAdvisoryLockSkipsCallbackWhileLockHeld(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}

	holder, err := pool.Begin(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() {
		if rollbackErr := holder.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback lock holder: %v", rollbackErr)
		}
	})

	_, err = holder.Exec(t.Context(), "SELECT pg_advisory_xact_lock($1)", int64(42))
	require.NoError(t, err)

	called := false

	require.NoError(t, store.WithAdvisoryLock(t.Context(), 42, func(context.Context, alarmDispatchMaintenanceDataStore) error {
		called = true

		return nil
	}))
	require.False(t, called, "다른 트랜잭션이 lock을 잡고 있으면 retention을 건너뛴다")
}

func TestAlarmDispatchMaintenancePGRetentionDeletesEveryTargetInLockedTransaction(t *testing.T) {
	pool := dbtest.NewPool(t)
	pgStore := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}
	store := &recordingAlarmDispatchMaintenanceStore{store: pgStore}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		retentionEnabled: true,
		queryTimeout:     5 * time.Second,
		limit:            1000,
		sentDays:         90,
		dlqDays:          180,
		quarantinedDays:  180,
		cancelledDays:    90,
		eventDays:        90,
		retentionLockKey: 42,
	}

	require.NoError(t, runner.RunOnce(t.Context()))
	// 종단 상태 네 개(sent·dlq·quarantined·취소)마다 한 번씩 지운다.
	require.Equal(t, 4, store.deletedTerminal)
	require.Equal(t, 1, store.deletedSendUnits)
	require.Equal(t, 1, store.deletedEvents)
}

func TestAlarmDispatchMaintenanceDeletesOnlyOrphanSendUnits(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}

	var orphanID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
		VALUES (repeat('a', 64), 'orphan-group', 'orphan-room', 'orphan-request')
		RETURNING id
	`).Scan(&orphanID))

	deleted, err := store.DeleteOrphanSendUnits(t.Context(), 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_send_units WHERE id = $1", orphanID).Scan(&remaining))
	require.Zero(t, remaining)
}

func TestAlarmDispatchRetentionDeleteSkipsRowRequeuedWhileWaitingForLock(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}

	dlqAt := time.Now().UTC().Add(-200 * 24 * time.Hour)
	eventID := seedAlarmDispatchRetentionEvent(t, pool)
	expiredID := seedAlarmDispatchRetentionDLQ(t, pool, eventID, "retention-expired", dlqAt)
	requeuedID := seedAlarmDispatchRetentionDLQ(t, pool, eventID, "retention-requeued", dlqAt)

	// requeue가 먼저 행을 잠근 채 dlq -> retry를 적용하고, retention DELETE는 그 잠금을 기다리게 합니다.
	requeueTx := beginAlarmDispatchRequeueHoldingLock(t, pool, requeuedID)
	done := make(chan alarmDispatchDeleteResult, 1)

	go func() {
		deleted, deleteErr := store.DeleteTerminal(ctx, dispatchoutbox.StatusDLQ, 180, 100)
		done <- alarmDispatchDeleteResult{deleted: deleted, err: deleteErr}
	}()

	waitForAlarmDispatchRetentionLockWait(t, pool)
	require.NoError(t, requeueTx.Commit(ctx))

	var result alarmDispatchDeleteResult

	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("retention DELETE did not finish after requeue commit")
	}

	require.NoError(t, result.err)
	require.EqualValues(t, 1, result.deleted)

	var expiredCount int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(id) FROM alarm_dispatch_deliveries WHERE id = $1", expiredID).Scan(&expiredCount))
	require.Zero(t, expiredCount)

	var requeuedStatus string

	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", requeuedID).Scan(&requeuedStatus))
	require.Equal(t, "retry", requeuedStatus)
}

type alarmDispatchDeleteResult struct {
	deleted int64
	err     error
}

func beginAlarmDispatchRequeueHoldingLock(t *testing.T, pool *pgxpool.Pool, deliveryID int64) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback requeue transaction: %v", rollbackErr)
		}
	})

	_, err = tx.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries
		SET status = 'retry', next_attempt_at = clock_timestamp()
		WHERE id = $1
	`, deliveryID)
	require.NoError(t, err)

	return tx
}

func waitForAlarmDispatchRetentionLockWait(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	require.Eventually(t, func() bool {
		var waiting bool

		scanErr := pool.QueryRow(t.Context(), `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%DELETE FROM alarm_dispatch_deliveries%'
			)
		`).Scan(&waiting)

		return scanErr == nil && waiting
	}, 10*time.Second, 20*time.Millisecond, "retention DELETE must wait on the requeue row lock")
}

func seedAlarmDispatchRetentionEvent(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_events (event_key, payload_hash, alarm_type, payload)
		VALUES ('retention-race-event', repeat('0', 64), 'LIVE'::alarm_type, '{}'::jsonb)
		RETURNING id
	`).Scan(&eventID))

	return eventID
}

func seedAlarmDispatchRetentionDLQ(t *testing.T, pool *pgxpool.Pool, eventID int64, dedupeKey string, dlqAt time.Time) int64 {
	t.Helper()

	var deliveryID int64

	// 운영 행처럼 send unit과 dispatch group을 함께 둡니다(send_unit_id는 NOT NULL).
	require.NoError(t, pool.QueryRow(t.Context(), `
		WITH unit AS (
			INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
			VALUES (encode(sha256(convert_to($2::text, 'UTF8')), 'hex'), 'group:' || $2::text, 'retention-room', 'retention-unit:' || $2::text)
			RETURNING id, dispatch_group_key
		)
		INSERT INTO alarm_dispatch_deliveries (
			event_id, room_id, dedupe_key, dispatch_group_key, send_unit_id, status, dlq_at, created_at, updated_at
		)
		SELECT $1, 'retention-room', $2::text, unit.dispatch_group_key, unit.id, 'dlq', $3, $3, $3
		FROM unit
		RETURNING id
	`, eventID, dedupeKey, dlqAt).Scan(&deliveryID))

	return deliveryID
}

type recordingAlarmDispatchMaintenanceStore struct {
	store            alarmDispatchMaintenancePgxStore
	deletedTerminal  int
	deletedSendUnits int
	deletedEvents    int
}

func (s *recordingAlarmDispatchMaintenanceStore) WithAdvisoryLock(
	ctx context.Context,
	key int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	if err := s.store.WithAdvisoryLock(ctx, key, func(lockedCtx context.Context, store alarmDispatchMaintenanceDataStore) error {
		return fn(lockedCtx, recordingAlarmDispatchMaintenanceDataStore{store: store, recorder: s})
	}); err != nil {
		return fmt.Errorf("with advisory lock: %w", err)
	}

	return nil
}

type recordingAlarmDispatchMaintenanceDataStore struct {
	store    alarmDispatchMaintenanceDataStore
	recorder *recordingAlarmDispatchMaintenanceStore
}

func (s recordingAlarmDispatchMaintenanceDataStore) DeleteTerminal(
	ctx context.Context,
	status dispatchoutbox.Status,
	retentionDays, limit int,
) (int64, error) {
	s.recorder.deletedTerminal++

	out, err := s.store.DeleteTerminal(ctx, status, retentionDays, limit)
	if err != nil {
		return out, fmt.Errorf("delete terminal: %w", err)
	}

	return out, nil
}

func (s recordingAlarmDispatchMaintenanceDataStore) DeleteOrphanEvents(
	ctx context.Context,
	retentionDays, limit int,
) (int64, error) {
	s.recorder.deletedEvents++

	out, err := s.store.DeleteOrphanEvents(ctx, retentionDays, limit)
	if err != nil {
		return out, fmt.Errorf("delete orphan events: %w", err)
	}

	return out, nil
}

func (s recordingAlarmDispatchMaintenanceDataStore) DeleteOrphanSendUnits(
	ctx context.Context,
	limit int,
) (int64, error) {
	s.recorder.deletedSendUnits++

	out, err := s.store.DeleteOrphanSendUnits(ctx, limit)
	if err != nil {
		return out, fmt.Errorf("delete orphan send units: %w", err)
	}

	return out, nil
}
