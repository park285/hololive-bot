package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

func rollbackAlarmDispatchTxOnPanic(ctx context.Context, tx pgx.Tx) {
	if p := recover(); p != nil {
		rollbackErr := pgxutil.Rollback(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("alarm dispatch retention transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}

		panic(p)
	}
}

func acquireAlarmDispatchLock(ctx context.Context, tx pgx.Tx, key int64) (bool, error) {
	var locked bool

	err := tx.QueryRow(ctx, mustSQL("alarm_dispatch_maintenance_0275_01.sql"), key).Scan(&locked)
	if err == nil {
		return locked, nil
	}

	if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil {
		return false, fmt.Errorf("acquire alarm dispatch retention transaction lock and rollback failed: %w", errors.Join(err, rollbackErr))
	}

	return false, fmt.Errorf("acquire alarm dispatch retention transaction lock: %w", err)
}

func rollbackAlarmDispatchTx(ctx context.Context, tx pgx.Tx, cause error, joinFmt string) error {
	if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil {
		return fmt.Errorf(joinFmt, errors.Join(cause, rollbackErr))
	}

	return cause
}

func (s alarmDispatchMaintenancePgxStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	snapshot := alarmDispatchBacklogSnapshot{RowsByStatus: map[dispatchoutbox.Status]int64{}}
	if err := s.loadBacklogRows(ctx, snapshot.RowsByStatus); err != nil {
		return snapshot, fmt.Errorf("load backlog rows: %w", err)
	}

	if err := s.loadOldestAges(ctx, &snapshot); err != nil {
		return snapshot, fmt.Errorf("load oldest ages: %w", err)
	}

	return snapshot, nil
}

func (s alarmDispatchMaintenancePgxStore) loadBacklogRows(ctx context.Context, out map[dispatchoutbox.Status]int64) error {
	rows, err := s.db.Query(ctx, mustSQL("alarm_dispatch_maintenance_0304_02.sql"))
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			status string
			count  int64
		)

		if err := rows.Scan(&status, &count); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		out[dispatchoutbox.Status(status)] = count
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	return nil
}

func (s alarmDispatchMaintenancePgxStore) loadOldestAges(ctx context.Context, snapshot *alarmDispatchBacklogSnapshot) error {
	if err := s.db.QueryRow(ctx, mustSQL("alarm_dispatch_maintenance_0325_03.sql")).
		Scan(
			&snapshot.OldestPendingAgeSeconds,
			&snapshot.OldestRetryAgeSeconds,
			&snapshot.OldestSendingAgeSeconds,
		); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	return nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteTerminal(
	ctx context.Context,
	status dispatchoutbox.Status,
	retentionDays, limit int,
) (int64, error) {
	column, ok := alarmDispatchTerminalTimestampColumn(status)
	if !ok {
		return 0, fmt.Errorf("unsupported alarm dispatch retention status: %s", status)
	}

	query := fmt.Sprintf(mustSQL("alarm_dispatch_maintenance_0348_04.sql"), column, column)

	tag, err := s.db.Exec(ctx, query, string(status), retentionDays, clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (int64, error) {
	tag, err := s.db.Exec(ctx, mustSQL("alarm_dispatch_maintenance_0368_05.sql"), retentionDays, clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanSendUnits(ctx context.Context, limit int) (int64, error) {
	tag, err := s.db.Exec(ctx, mustSQL("alarm_dispatch_maintenance_0360_05.sql"), clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}
