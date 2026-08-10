package dispatchrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/require"
)

type recordingAlarmDispatchRollbackTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingAlarmDispatchRollbackTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

func TestRollbackAlarmDispatchTxOnPanicPreservesPanicWhenRollbackFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingAlarmDispatchRollbackTx{rollbackErr: errors.New("rollback failed")}
	panicValue := &struct{ message string }{message: "alarm dispatch panic"}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		defer rollbackAlarmDispatchTxOnPanic(ctx, tx)
		panic(panicValue)
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}

func TestAlarmDispatchMaintenancePGObservationFailureDoesNotContaminateDeletionTransaction(t *testing.T) {
	tests := []struct {
		name    string
		timeout bool
	}{
		{name: "immediate error"},
		{name: "timeout", timeout: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			pgStore := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}
			store := &recordingAlarmDispatchMaintenanceStore{store: pgStore}
			runner := &alarmDispatchMaintenanceRunner{
				store:            store,
				observerStore:    failingAlarmDispatchPGObserver{pool: pool, timeout: tt.timeout},
				retentionEnabled: true,
				queryTimeout:     250 * time.Millisecond,
				limit:            1000,
				retentionLockKey: 42,
			}

			require.NoError(t, runner.RunOnce(t.Context()))
			require.Equal(t, 5, store.deletedTerminal)
			require.Equal(t, 1, store.deletedSendUnits)
			require.Equal(t, 1, store.deletedEvents)
		})
	}
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

type failingAlarmDispatchPGObserver struct {
	pool    *pgxpool.Pool
	timeout bool
}

func (s failingAlarmDispatchPGObserver) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	if s.timeout {
		var ignored any
		err := s.pool.QueryRow(ctx, "SELECT pg_sleep(1)").Scan(&ignored)
		return alarmDispatchBacklogSnapshot{}, err
	}
	_, err := s.pool.Exec(ctx, "SELECT 1 / 0")
	return alarmDispatchBacklogSnapshot{}, err
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
	return s.store.WithAdvisoryLock(ctx, key, func(lockedCtx context.Context, store alarmDispatchMaintenanceDataStore) error {
		return fn(lockedCtx, recordingAlarmDispatchMaintenanceDataStore{store: store, recorder: s})
	})
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
	return s.store.DeleteTerminal(ctx, status, retentionDays, limit)
}

func (s recordingAlarmDispatchMaintenanceDataStore) DeleteOrphanEvents(
	ctx context.Context,
	retentionDays, limit int,
) (int64, error) {
	s.recorder.deletedEvents++
	return s.store.DeleteOrphanEvents(ctx, retentionDays, limit)
}

func (s recordingAlarmDispatchMaintenanceDataStore) DeleteOrphanSendUnits(
	ctx context.Context,
	limit int,
) (int64, error) {
	s.recorder.deletedSendUnits++
	return s.store.DeleteOrphanSendUnits(ctx, limit)
}
