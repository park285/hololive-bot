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

const (
	alarmDispatchRetentionMaxLimit = 10000
	alarmDispatchRetentionLockKey  = 781512042
)

// 종료 상태마다 상태와 보존 시각 열을 상수로 쓴 삭제 SQL을 둔다. 상태를 파라미터로 받으면 generic plan이
// 상태별 보존 부분 인덱스를 쓰지 못하고 해당 상태 전체를 읽는다.
var alarmDispatchRetentionDeleteSQL = map[dispatchoutbox.Status]string{
	dispatchoutbox.StatusSent:        mustSQL("alarm_dispatch_retention_delete_sent.sql"),
	dispatchoutbox.StatusDLQ:         mustSQL("alarm_dispatch_retention_delete_dlq.sql"),
	dispatchoutbox.StatusQuarantined: mustSQL("alarm_dispatch_retention_delete_quarantined.sql"),
	dispatchoutbox.StatusCancelled:   mustSQL("alarm_dispatch_retention_delete_cancelled.sql"), //nolint:misspell // 파일명이 실제 상태값 cancelled를 따른다.
}

type alarmDispatchMaintenanceStore interface {
	WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context, alarmDispatchMaintenanceDataStore) error) error
}

type alarmDispatchMaintenanceDataStore interface {
	DeleteTerminal(ctx context.Context, status dispatchoutbox.Status, retentionDays, limit int) (int64, error)
	DeleteOrphanSendUnits(ctx context.Context, limit int) (int64, error)
	DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (int64, error)
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

func NewMaintenanceRunner(
	infra *sharedmodules.InfraModule,
	retentionConfig workerconfig.DispatchRetentionConfig,
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
	if r.store == nil {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("retention sweep aborted: %w", err)
	}

	if !r.retentionEnabled {
		return nil
	}

	deleteCtx, cancelDelete := context.WithTimeout(ctx, r.queryTimeout)

	defer cancelDelete()

	if err := r.store.WithAdvisoryLock(deleteCtx, r.retentionLockKey, r.deleteRetainedRows); err != nil {
		return fmt.Errorf("sweep alarm dispatch retention: %w", err)
	}

	return nil
}

func (r *alarmDispatchMaintenanceRunner) deleteRetainedRows(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
	for _, target := range r.retentionTargets() {
		rows, err := store.DeleteTerminal(ctx, target.status, target.retentionDays, r.limit)
		if err != nil {
			return fmt.Errorf("delete retained alarm dispatch %s rows: %w", target.status, err)
		}

		observeAlarmDispatchRetentionDeletedRows(string(target.status), rows)
	}

	rows, err := store.DeleteOrphanSendUnits(ctx, r.limit)
	if err != nil {
		return fmt.Errorf("delete retained orphan alarm dispatch send units: %w", err)
	}

	observeAlarmDispatchRetentionDeletedRows("send_unit", rows)

	rows, err = store.DeleteOrphanEvents(ctx, r.eventDays, r.limit)
	if err != nil {
		return fmt.Errorf("delete retained orphan alarm dispatch events: %w", err)
	}

	observeAlarmDispatchRetentionDeletedRows("event", rows)

	return nil
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

// WithAdvisoryLock은 트랜잭션 advisory lock을 얻은 경우에만 fn을 같은 트랜잭션에서 실행한다.
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

	if err := tx.QueryRow(ctx, mustSQL("alarm_dispatch_maintenance_0275_01.sql"), key).Scan(&locked); err != nil {
		return false, fmt.Errorf("scan: %w", err)
	}

	return locked, nil
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
			&snapshot.QuarantinedRows,
			&snapshot.OldestQuarantinedAgeSeconds,
			&snapshot.UnreviewedQuarantinedRows,
			&snapshot.OldestUnreviewedQuarantinedAgeSeconds,
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
	query, ok := alarmDispatchRetentionDeleteSQL[status]
	if !ok {
		return 0, fmt.Errorf("unsupported alarm dispatch retention status: %s", status)
	}

	// picked CTE는 행을 잠그지 않고 EvalPlanQual 때 다시 계산되지 않습니다. 바깥 DELETE가 status와
	// 보존 시각을 다시 검사해야 대기 중에 requeue로 commit된 행을 최신 버전 기준으로 건너뜁니다.
	tag, err := s.db.Exec(ctx, query, retentionDays, clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s alarmDispatchMaintenancePgxStore) DeleteOrphanEvents(ctx context.Context, retentionDays, limit int) (int64, error) {
	deletedCandidates, err := dispatchoutbox.NewUpcomingCandidates(s.db).Cleanup(ctx, retentionDays, clampAlarmDispatchRetentionLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("cleanup upcoming candidates: %w", err)
	}

	observeAlarmDispatchRetentionDeletedRows("upcoming_candidates", deletedCandidates)

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

func clampAlarmDispatchRetentionLimit(limit int) int {
	if limit <= 0 {
		return 1000
	}

	if limit > alarmDispatchRetentionMaxLimit {
		return alarmDispatchRetentionMaxLimit
	}

	return limit
}
