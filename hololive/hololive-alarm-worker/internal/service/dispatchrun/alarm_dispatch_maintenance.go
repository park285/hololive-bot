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
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

const (
	alarmDispatchRetentionMaxLimit   = 10000
	alarmDispatchRetentionLockKey    = 781512042
	alarmDispatchShadowRetentionDays = 14
)

var alarmDispatchTerminalTimestampColumns = map[dispatchoutbox.Status]string{
	dispatchoutbox.StatusShadowed:    "created_at",
	dispatchoutbox.StatusSent:        "sent_at",
	dispatchoutbox.StatusDLQ:         "dlq_at",
	dispatchoutbox.StatusQuarantined: "quarantined_at",
	dispatchoutbox.StatusCancelled:   "cancelled_at", //nolint:misspell // alarm_dispatch_deliveries의 실제 컬럼명이 영국식 cancelled_at이라 canceled로 바꾸면 쿼리가 깨진다.
}

type alarmDispatchMaintenanceStore interface {
	WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context, alarmDispatchMaintenanceDataStore) error) error
}

type alarmDispatchMaintenanceObserverStore interface {
	BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error)
}

type alarmDispatchMaintenanceDataStore interface {
	DeleteTerminal(ctx context.Context, status dispatchoutbox.Status, retentionDays, limit int) (int64, error)
	DeleteOrphanSendUnits(ctx context.Context, limit int) (int64, error)
	DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (int64, error)
}

type alarmDispatchBacklogSnapshot struct {
	RowsByStatus            map[dispatchoutbox.Status]int64
	OldestPendingAgeSeconds float64
	OldestRetryAgeSeconds   float64
	OldestSendingAgeSeconds float64
}

type alarmDispatchMaintenanceRunner struct {
	store            alarmDispatchMaintenanceStore
	observerStore    alarmDispatchMaintenanceObserverStore
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
	retentionConfig settings.AlarmDispatchRetentionConfig,
	logger *slog.Logger,
) Scheduler {
	if infra == nil || infra.Postgres == nil {
		return nil
	}

	pool := infra.Postgres.GetPool()
	if pool == nil {
		return nil
	}

	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}

	return &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
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
			r.reportFailure(ctx, err)
		}

		if !retry.Sleep(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r *alarmDispatchMaintenanceRunner) reportFailure(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}

	observeAlarmDispatchRetentionFailure()

	if r.logger != nil {
		r.logger.Warn("Alarm dispatch maintenance failed", slog.Any("error", err))
	}
}

func (r *alarmDispatchMaintenanceRunner) RunOnce(ctx context.Context) error {
	if r.store == nil {
		return nil
	}

	r.observeBacklogOnce(ctx)

	if !r.retentionEnabled || ctx.Err() != nil {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retention sweep aborted: %w", err)
		}

		return nil
	}

	deleteCtx, cancelDelete := context.WithTimeout(ctx, r.effectiveQueryTimeout())

	defer cancelDelete()

	if err := r.store.WithAdvisoryLock(deleteCtx, r.effectiveLockKey(), r.deleteRetainedRows); err != nil {
		return fmt.Errorf("with advisory lock: %w", err)
	}

	return nil
}

func (r *alarmDispatchMaintenanceRunner) observeBacklogOnce(ctx context.Context) {
	if r.observerStore != nil {
		observeCtx, cancelObserve := context.WithTimeout(ctx, r.effectiveQueryTimeout())
		err := r.observeBacklog(observeCtx, r.observerStore)

		cancelObserve()

		if err != nil && ctx.Err() == nil {
			observeAlarmDispatchBacklogObservationFailure()

			if r.logger != nil {
				r.logger.Warn("alarm dispatch backlog observation failed", slog.Any("error", err))
			}
		}
	}
}

func (r *alarmDispatchMaintenanceRunner) deleteRetainedRows(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
	for _, target := range r.retentionTargets() {
		rows, err := store.DeleteTerminal(ctx, target.status, target.retentionDays, r.effectiveLimit())
		if err != nil {
			return fmt.Errorf("delete retained alarm dispatch %s rows: %w", target.status, err)
		}

		observeAlarmDispatchRetentionDeletedRows(string(target.status), rows)
	}

	rows, err := store.DeleteOrphanSendUnits(ctx, r.effectiveLimit())
	if err != nil {
		return fmt.Errorf("delete retained orphan alarm dispatch send units: %w", err)
	}

	observeAlarmDispatchRetentionDeletedRows("send_unit", rows)

	rows, err = store.DeleteOrphanEvents(ctx, r.effectiveEventDays(), r.effectiveLimit())
	if err != nil {
		return fmt.Errorf("delete retained orphan alarm dispatch events: %w", err)
	}

	observeAlarmDispatchRetentionDeletedRows("event", rows)

	return nil
}

func (r *alarmDispatchMaintenanceRunner) observeBacklog(ctx context.Context, store alarmDispatchMaintenanceObserverStore) error {
	snapshot, err := store.BacklogSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("backlog snapshot: %w", err)
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
		{status: dispatchoutbox.StatusShadowed, retentionDays: alarmDispatchShadowRetentionDays},
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
		return errors.New("alarm dispatch maintenance pgx pool is nil")
	}

	tx, err := s.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin alarm dispatch retention transaction: %w", err)
	}

	defer rollbackAlarmDispatchTxOnPanic(ctx, tx)

	locked, err := acquireAlarmDispatchLock(ctx, tx, key)
	if err != nil {
		return fmt.Errorf("acquire alarm dispatch lock: %w", err)
	}

	if locked && fn != nil {
		err = fn(ctx, alarmDispatchMaintenancePgxStore{db: tx})
	}

	if err != nil {
		//nolint:wrapcheck // rollbackAlarmDispatchTx는 롤백이 성공하면 원인 오류를, 실패하면 둘을 합친 오류를 이미 완결되게 돌려준다.
		return rollbackAlarmDispatchTx(ctx, tx, err, "alarm dispatch retention transaction failed and rollback failed: %w")
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit alarm dispatch retention transaction: %w", err)
	}

	return nil
}
