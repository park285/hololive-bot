package alarmdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/retry"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/dbx"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

const alarmDispatchRetentionLockKey = 781512042

type alarmDispatchMaintenanceStore interface {
	WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context, alarmDispatchMaintenanceDataStore) error) error
}

type alarmDispatchMaintenanceDataStore interface {
	DeleteTerminal(ctx context.Context, status dispatchoutbox.Status, retentionDays, limit int) (int64, error)
	DeleteOrphanSendUnits(ctx context.Context, limit int) (int64, error)
	DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (alarmDispatchOrphanEventCounts, error)
}

type alarmDispatchOrphanEventCounts struct {
	events             int64
	upcomingCandidates int64
}

type alarmDispatchRetentionCounts struct {
	terminal  [4]int64
	sendUnits int64
	orphans   alarmDispatchOrphanEventCounts
}

type alarmDispatchBacklogSnapshot struct {
	RowsByStatus                          map[dispatchoutbox.Status]int64
	OldestPendingAgeSeconds               float64
	OldestRetryAgeSeconds                 float64
	OldestSendingAgeSeconds               float64
	QuarantinedRows                       int64
	OldestQuarantinedAgeSeconds           float64
	UnreviewedQuarantinedRows             int64
	OldestUnreviewedQuarantinedAgeSeconds float64
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

// NewMaintenanceRunner는 PostgreSQL pool이 있는 infra를 받는다. 조립 쪽(buildNotificationEgress)이 기동 때 pool을 먼저
// 확인하고, 보존 limit은 config가 1~10000으로 맞춘 값을 SQL LIMIT으로 그대로 쓴다.
func NewMaintenanceRunner(
	infra *sharedmodules.InfraModule,
	retentionConfig workerconfig.DispatchRetentionConfig,
	logger *slog.Logger,
) Scheduler {
	pool := infra.Postgres.GetPool()

	return &alarmDispatchMaintenanceRunner{
		store:            alarmDispatchMaintenancePgxStore{db: pool, beginner: pool},
		retentionEnabled: retentionConfig.Enabled,
		interval:         retentionConfig.Interval,
		queryTimeout:     retentionConfig.QueryTimeout,
		limit:            retentionConfig.Limit,
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

		if !retry.Sleep(ctx, r.interval) {
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
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("retention sweep aborted: %w", err)
	}

	if !r.retentionEnabled {
		return nil
	}

	deleteCtx, cancelDelete := context.WithTimeout(ctx, r.queryTimeout)

	defer cancelDelete()

	var (
		counts   alarmDispatchRetentionCounts
		executed bool
	)

	if err := r.store.WithAdvisoryLock(deleteCtx, r.retentionLockKey, func(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
		var err error

		counts, err = r.deleteRetainedRows(ctx, store)
		executed = true

		return err
	}); err != nil {
		return fmt.Errorf("sweep alarm dispatch retention: %w", err)
	}

	// lock 미획득과 COMMIT 결과 불명은 삭제 확정이 아니므로 계상하거나 재시도하지 않습니다.
	if executed {
		for i, target := range r.retentionTargets() {
			observeAlarmDispatchRetentionDeletedRows(string(target.status), counts.terminal[i])
		}

		observeAlarmDispatchRetentionDeletedRows("send_unit", counts.sendUnits)
		observeAlarmDispatchRetentionDeletedRows("event", counts.orphans.events)
		observeAlarmDispatchRetentionDeletedRows("upcoming_candidates", counts.orphans.upcomingCandidates)
	}

	return nil
}

func (r *alarmDispatchMaintenanceRunner) deleteRetainedRows(ctx context.Context, store alarmDispatchMaintenanceDataStore) (alarmDispatchRetentionCounts, error) {
	var counts alarmDispatchRetentionCounts

	for i, target := range r.retentionTargets() {
		rows, err := store.DeleteTerminal(ctx, target.status, target.retentionDays, r.limit)
		if err != nil {
			return counts, fmt.Errorf("delete retained alarm dispatch %s rows: %w", target.status, err)
		}

		counts.terminal[i] = rows
	}

	rows, err := store.DeleteOrphanSendUnits(ctx, r.limit)
	if err != nil {
		return counts, fmt.Errorf("delete retained orphan alarm dispatch send units: %w", err)
	}

	counts.sendUnits = rows

	counts.orphans, err = store.DeleteOrphanEvents(ctx, r.eventDays, r.limit)
	if err != nil {
		return counts, fmt.Errorf("delete retained orphan alarm dispatch events: %w", err)
	}

	return counts, nil
}

func (r *alarmDispatchMaintenanceRunner) retentionTargets() []alarmDispatchRetentionTarget {
	return []alarmDispatchRetentionTarget{
		{status: dispatchoutbox.StatusSent, retentionDays: r.sentDays},
		{status: dispatchoutbox.StatusDLQ, retentionDays: r.dlqDays},
		{status: dispatchoutbox.StatusQuarantined, retentionDays: r.quarantinedDays},
		{status: dispatchoutbox.StatusCancelled, retentionDays: r.cancelledDays},
	}
}

type alarmDispatchRetentionTarget struct {
	status        dispatchoutbox.Status
	retentionDays int
}

// 다른 인스턴스가 lock을 잡고 있으면 fn 없이 성공으로 끝난다.
func (s alarmDispatchMaintenancePgxStore) WithAdvisoryLock(
	ctx context.Context,
	key int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	err := dbx.InPgxTx(ctx, s.beginner, func(tx dbx.Tx) error {
		locked, err := acquireAlarmDispatchLock(ctx, tx, key)
		if err != nil {
			return fmt.Errorf("acquire alarm dispatch lock: %w", err)
		}

		if !locked || fn == nil {
			return nil
		}

		return fn(ctx, alarmDispatchMaintenancePgxStore{db: tx})
	})
	if err != nil {
		return fmt.Errorf("run alarm dispatch retention transaction: %w", err)
	}

	return nil
}

func acquireAlarmDispatchLock(ctx context.Context, tx dbx.Tx, key int64) (bool, error) {
	var locked bool

	if err := tx.QueryRow(ctx, acquireAlarmDispatchLockSQL, key).Scan(&locked); err != nil {
		return false, fmt.Errorf("scan: %w", err)
	}

	return locked, nil
}

func (s alarmDispatchMaintenancePgxStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	snapshot := alarmDispatchBacklogSnapshot{}

	var pendingRows, retryRows, leasedRows, sendingRows int64

	if err := s.db.QueryRow(ctx, backlogSnapshotSQL).
		Scan(
			&snapshot.OldestPendingAgeSeconds,
			&snapshot.OldestRetryAgeSeconds,
			&snapshot.OldestSendingAgeSeconds,
			&snapshot.QuarantinedRows,
			&snapshot.OldestQuarantinedAgeSeconds,
			&snapshot.UnreviewedQuarantinedRows,
			&snapshot.OldestUnreviewedQuarantinedAgeSeconds,
			&pendingRows,
			&retryRows,
			&leasedRows,
			&sendingRows,
		); err != nil {
		return snapshot, fmt.Errorf("load backlog snapshot: scan: %w", err)
	}

	snapshot.RowsByStatus = map[dispatchoutbox.Status]int64{
		dispatchoutbox.StatusPending: pendingRows,
		dispatchoutbox.StatusRetry:   retryRows,
		dispatchoutbox.StatusLeased:  leasedRows,
		dispatchoutbox.StatusSending: sendingRows,
	}

	return snapshot, nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteTerminal(
	ctx context.Context,
	status dispatchoutbox.Status,
	retentionDays, limit int,
) (int64, error) {
	query, ok := alarmDispatchRetentionDeleteSQL[status]
	if !ok {
		return 0, fmt.Errorf("unsupported alarm dispatch retention status: %s", status)
	}

	// picked CTE는 행을 잠그지 않고 EvalPlanQual 때 다시 계산되지 않습니다. 바깥 DELETE가 status와
	// 보존 시각을 다시 검사해야 대기 중에 requeue로 commit된 행을 최신 버전 기준으로 건너뜁니다.
	tag, err := s.db.Exec(ctx, query, retentionDays, limit)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (alarmDispatchOrphanEventCounts, error) {
	deletedCandidates, err := dispatchoutbox.NewUpcomingCandidates(s.db).Cleanup(ctx, retentionDays, limit)
	if err != nil {
		return alarmDispatchOrphanEventCounts{}, fmt.Errorf("cleanup upcoming candidates: %w", err)
	}

	tag, err := s.db.Exec(ctx, deleteOrphanEventsSQL, retentionDays, limit)
	if err != nil {
		return alarmDispatchOrphanEventCounts{}, fmt.Errorf("exec: %w", err)
	}

	return alarmDispatchOrphanEventCounts{events: tag.RowsAffected(), upcomingCandidates: deletedCandidates}, nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanSendUnits(ctx context.Context, limit int) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteOrphanSendUnitsSQL, limit)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}
