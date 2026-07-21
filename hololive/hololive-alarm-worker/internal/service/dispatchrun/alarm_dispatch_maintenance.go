package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/park285/shared-go/pkg/retry"
)

const (
	alarmDispatchRetentionMaxLimit = 10000
	alarmDispatchRetentionLockKey  = 781512042
)

var alarmDispatchTerminalTimestampColumns = map[dispatchoutbox.Status]string{
	dispatchoutbox.StatusSent:        "sent_at",
	dispatchoutbox.StatusDLQ:         "dlq_at",
	dispatchoutbox.StatusQuarantined: "quarantined_at",
	dispatchoutbox.StatusCancelled:   "cancelled_at",
}

type alarmDispatchMaintenanceStore interface {
	WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context, alarmDispatchMaintenanceDataStore) error) error
}

type alarmDispatchMaintenanceDataStore interface {
	BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error)
	DeleteTerminal(ctx context.Context, status dispatchoutbox.Status, retentionDays int, limit int) (int64, error)
	DeleteOrphanEvents(ctx context.Context, retentionDays int, limit int) (int64, error)
}

type alarmDispatchBacklogSnapshot struct {
	RowsByStatus            map[dispatchoutbox.Status]int64
	OldestPendingAgeSeconds float64
	OldestRetryAgeSeconds   float64
	OldestSendingAgeSeconds float64
}

type alarmDispatchMaintenanceRunner struct {
	store            alarmDispatchMaintenanceStore
	retentionEnabled bool
	interval         time.Duration
	queryTimeout     time.Duration
	limit            int
	sentDays         int
	dlqDays          int
	quarantinedDays  int
	cancelledDays    int
	eventDays        int
	retentionLockKey int64
	logger           *slog.Logger
}

type alarmDispatchMaintenanceQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type alarmDispatchMaintenancePgxStore struct {
	db       alarmDispatchMaintenanceQuerier
	beginner *pgxpool.Pool
}

func NewMaintenanceRunner(
	infra *sharedmodules.InfraModule,
	retentionConfig config.AlarmDispatchRetentionConfig,
	logger *slog.Logger,
) Scheduler {
	if infra == nil || infra.Postgres == nil {
		return nil
	}
	pool := infra.Postgres.GetPool()
	if pool == nil {
		return nil
	}
	return &alarmDispatchMaintenanceRunner{
		store:            alarmDispatchMaintenancePgxStore{db: pool, beginner: pool},
		retentionEnabled: retentionConfig.Enabled,
		interval:         retentionConfig.Interval,
		queryTimeout:     retentionConfig.QueryTimeout,
		limit:            clampAlarmDispatchRetentionLimit(retentionConfig.Limit),
		sentDays:         retentionConfig.SentDays,
		dlqDays:          retentionConfig.DLQDays,
		quarantinedDays:  retentionConfig.QuarantinedDays,
		cancelledDays:    retentionConfig.CancelledDays,
		eventDays:        retentionConfig.EventDays,
		retentionLockKey: alarmDispatchRetentionLockKey,
		logger:           logger,
	}
}

func (r *alarmDispatchMaintenanceRunner) Start(ctx context.Context) error {
	for {
		if err := r.RunOnce(ctx); err != nil {
			observeAlarmDispatchRetentionFailure()
			if r.logger != nil {
				r.logger.Warn("Alarm dispatch maintenance failed", slog.Any("error", err))
			}
		}
		if !retry.Sleep(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r *alarmDispatchMaintenanceRunner) RunOnce(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, r.effectiveQueryTimeout())
	defer cancel()
	return r.store.WithAdvisoryLock(queryCtx, r.effectiveLockKey(), func(lockedCtx context.Context, store alarmDispatchMaintenanceDataStore) error {
		if err := r.observeBacklog(lockedCtx, store); err != nil {
			return fmt.Errorf("observe alarm dispatch backlog: %w", err)
		}
		if !r.retentionEnabled {
			return nil
		}
		return r.deleteRetainedRows(lockedCtx, store)
	})
}

func (r *alarmDispatchMaintenanceRunner) deleteRetainedRows(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
	for _, target := range r.retentionTargets() {
		rows, err := store.DeleteTerminal(ctx, target.status, target.retentionDays, r.effectiveLimit())
		if err != nil {
			return fmt.Errorf("delete retained alarm dispatch %s rows: %w", target.status, err)
		}
		observeAlarmDispatchRetentionDeletedRows(string(target.status), rows)
	}
	rows, err := store.DeleteOrphanEvents(ctx, r.effectiveEventDays(), r.effectiveLimit())
	if err != nil {
		return fmt.Errorf("delete retained orphan alarm dispatch events: %w", err)
	}
	observeAlarmDispatchRetentionDeletedRows("event", rows)
	return nil
}

func (r *alarmDispatchMaintenanceRunner) observeBacklog(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
	snapshot, err := store.BacklogSnapshot(ctx)
	if err != nil {
		return err
	}
	for _, status := range []dispatchoutbox.Status{
		dispatchoutbox.StatusPending,
		dispatchoutbox.StatusRetry,
		dispatchoutbox.StatusLeased,
		dispatchoutbox.StatusSending,
	} {
		observeAlarmDispatchBacklogStatus(string(status), snapshot.RowsByStatus[status])
	}
	observeAlarmDispatchOldestAges(
		snapshot.OldestPendingAgeSeconds,
		snapshot.OldestRetryAgeSeconds,
		snapshot.OldestSendingAgeSeconds,
	)
	return nil
}

func (r *alarmDispatchMaintenanceRunner) retentionTargets() []alarmDispatchRetentionTarget {
	return []alarmDispatchRetentionTarget{
		{status: dispatchoutbox.StatusSent, retentionDays: r.effectiveDays(r.sentDays, 90)},
		{status: dispatchoutbox.StatusDLQ, retentionDays: r.effectiveDays(r.dlqDays, 180)},
		{status: dispatchoutbox.StatusQuarantined, retentionDays: r.effectiveDays(r.quarantinedDays, 180)},
		{status: dispatchoutbox.StatusCancelled, retentionDays: r.effectiveDays(r.cancelledDays, 90)},
	}
}

type alarmDispatchRetentionTarget struct {
	status        dispatchoutbox.Status
	retentionDays int
}

func (r *alarmDispatchMaintenanceRunner) effectiveInterval() time.Duration {
	if r.interval > 0 {
		return r.interval
	}
	return time.Hour
}

func (r *alarmDispatchMaintenanceRunner) effectiveQueryTimeout() time.Duration {
	if r.queryTimeout > 0 {
		return r.queryTimeout
	}
	return 30 * time.Second
}

func (r *alarmDispatchMaintenanceRunner) effectiveLimit() int {
	return clampAlarmDispatchRetentionLimit(r.limit)
}

func (r *alarmDispatchMaintenanceRunner) effectiveEventDays() int {
	return r.effectiveDays(r.eventDays, 90)
}

func (r *alarmDispatchMaintenanceRunner) effectiveLockKey() int64 {
	if r.retentionLockKey != 0 {
		return r.retentionLockKey
	}
	return alarmDispatchRetentionLockKey
}

func (r *alarmDispatchMaintenanceRunner) effectiveDays(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func clampAlarmDispatchRetentionLimit(limit int) int {
	if limit <= 0 {
		return 1000
	}
	if limit > alarmDispatchRetentionMaxLimit {
		return alarmDispatchRetentionMaxLimit
	}
	return limit
}

func alarmDispatchTerminalTimestampColumn(status dispatchoutbox.Status) (string, bool) {
	column, ok := alarmDispatchTerminalTimestampColumns[status]
	return column, ok
}

func (s alarmDispatchMaintenancePgxStore) WithAdvisoryLock(
	ctx context.Context,
	key int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	if s.beginner == nil {
		return fmt.Errorf("alarm dispatch maintenance pgx pool is nil")
	}

	tx, err := s.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin alarm dispatch retention transaction: %w", err)
	}

	defer rollbackAlarmDispatchTxOnPanic(ctx, tx)

	locked, err := acquireAlarmDispatchLock(ctx, tx, key)
	if err != nil {
		return err
	}
	if locked && fn != nil {
		err = fn(ctx, alarmDispatchMaintenancePgxStore{db: tx})
	}
	if err != nil {
		return rollbackAlarmDispatchTx(ctx, tx, err, "alarm dispatch retention transaction failed and rollback failed: %w")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit alarm dispatch retention transaction: %w", err)
	}
	return nil
}

func rollbackAlarmDispatchTxOnPanic(ctx context.Context, tx pgx.Tx) {
	if p := recover(); p != nil {
		if err := rollbackAlarmDispatchTx(ctx, tx, nil, "alarm dispatch retention transaction panic rollback failed: %w"); err != nil {
			panic(fmt.Errorf("alarm dispatch retention transaction panic: %v: %w", p, err))
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
		return snapshot, err
	}
	if err := s.loadOldestAges(ctx, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (s alarmDispatchMaintenancePgxStore) loadBacklogRows(ctx context.Context, out map[dispatchoutbox.Status]int64) error {
	rows, err := s.db.Query(ctx, mustSQL("alarm_dispatch_maintenance_0304_02.sql"))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		out[dispatchoutbox.Status(status)] = count
	}
	return rows.Err()
}

func (s alarmDispatchMaintenancePgxStore) loadOldestAges(ctx context.Context, snapshot *alarmDispatchBacklogSnapshot) error {
	return s.db.QueryRow(ctx, mustSQL("alarm_dispatch_maintenance_0325_03.sql")).
		Scan(
			&snapshot.OldestPendingAgeSeconds,
			&snapshot.OldestRetryAgeSeconds,
			&snapshot.OldestSendingAgeSeconds,
		)
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
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (int64, error) {
	tag, err := s.db.Exec(ctx, mustSQL("alarm_dispatch_maintenance_0368_05.sql"), retentionDays, clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
