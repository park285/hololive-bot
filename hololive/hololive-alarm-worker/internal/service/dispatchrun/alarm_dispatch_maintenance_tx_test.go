package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
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
	ctx, cancel := context.WithCancel(t.Context())
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

		if err := s.pool.QueryRow(ctx, "SELECT pg_sleep(1)").Scan(&ignored); err != nil {
			return alarmDispatchBacklogSnapshot{}, fmt.Errorf("observe alarm dispatch backlog with sleep: %w", err)
		}

		return alarmDispatchBacklogSnapshot{}, nil
	}

	if _, err := s.pool.Exec(ctx, "SELECT 1 / 0"); err != nil {
		return alarmDispatchBacklogSnapshot{}, fmt.Errorf("observe alarm dispatch backlog: %w", err)
	}

	return alarmDispatchBacklogSnapshot{}, nil
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
