package workerapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"gorm.io/gorm"
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

type alarmDispatchMaintenanceGormStore struct {
	db *gorm.DB
}

func buildAlarmDispatchMaintenanceRunner(
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if infra == nil || infra.Postgres == nil {
		return nil
	}
	db := infra.Postgres.GetGormDB()
	if db == nil {
		return nil
	}
	return alarmDispatchMaintenanceRunner{
		store:            alarmDispatchMaintenanceGormStore{db: db},
		retentionEnabled: parseBoolEnv("ALARM_DISPATCH_RETENTION_ENABLED", true),
		interval:         parsePositiveDurationMSEnv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", time.Hour),
		queryTimeout:     parsePositiveDurationMSEnv("ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS", 30*time.Second),
		limit:            clampAlarmDispatchRetentionLimit(parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_LIMIT", 1000)),
		sentDays:         parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_SENT_DAYS", 90),
		dlqDays:          parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_DLQ_DAYS", 180),
		quarantinedDays:  parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS", 180),
		cancelledDays:    parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", 90),
		eventDays:        parsePositiveIntEnv("ALARM_DISPATCH_RETENTION_EVENT_DAYS", 90),
		retentionLockKey: alarmDispatchRetentionLockKey,
		logger:           logger,
	}
}

func (r alarmDispatchMaintenanceRunner) Start(ctx context.Context) error {
	for {
		if err := r.RunOnce(ctx); err != nil {
			observeAlarmDispatchRetentionFailure()
			if r.logger != nil {
				r.logger.Warn("Alarm dispatch maintenance failed", slog.Any("error", err))
			}
		}
		if !sleepContext(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r alarmDispatchMaintenanceRunner) RunOnce(ctx context.Context) error {
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

func (r alarmDispatchMaintenanceRunner) deleteRetainedRows(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
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

func (r alarmDispatchMaintenanceRunner) observeBacklog(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
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

func (r alarmDispatchMaintenanceRunner) retentionTargets() []alarmDispatchRetentionTarget {
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

func (r alarmDispatchMaintenanceRunner) effectiveInterval() time.Duration {
	if r.interval > 0 {
		return r.interval
	}
	return time.Hour
}

func (r alarmDispatchMaintenanceRunner) effectiveQueryTimeout() time.Duration {
	if r.queryTimeout > 0 {
		return r.queryTimeout
	}
	return 30 * time.Second
}

func (r alarmDispatchMaintenanceRunner) effectiveLimit() int {
	return clampAlarmDispatchRetentionLimit(r.limit)
}

func (r alarmDispatchMaintenanceRunner) effectiveEventDays() int {
	return r.effectiveDays(r.eventDays, 90)
}

func (r alarmDispatchMaintenanceRunner) effectiveLockKey() int64 {
	if r.retentionLockKey != 0 {
		return r.retentionLockKey
	}
	return alarmDispatchRetentionLockKey
}

func (r alarmDispatchMaintenanceRunner) effectiveDays(value, fallback int) int {
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

func (s alarmDispatchMaintenanceGormStore) WithAdvisoryLock(
	ctx context.Context,
	key int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?)", key).Scan(&locked).Error; err != nil {
			return fmt.Errorf("acquire alarm dispatch retention transaction lock: %w", err)
		}
		if !locked {
			return nil
		}
		return fn(ctx, alarmDispatchMaintenanceGormStore{db: tx})
	})
}

func (s alarmDispatchMaintenanceGormStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	snapshot := alarmDispatchBacklogSnapshot{RowsByStatus: map[dispatchoutbox.Status]int64{}}
	if err := s.loadBacklogRows(ctx, snapshot.RowsByStatus); err != nil {
		return snapshot, err
	}
	if err := s.loadOldestAges(ctx, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (s alarmDispatchMaintenanceGormStore) loadBacklogRows(ctx context.Context, out map[dispatchoutbox.Status]int64) error {
	rows, err := s.db.WithContext(ctx).Raw(`
SELECT status, COUNT(*) AS rows
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status`).Rows()
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

func (s alarmDispatchMaintenanceGormStore) loadOldestAges(ctx context.Context, snapshot *alarmDispatchBacklogSnapshot) error {
	return s.db.WithContext(ctx).Raw(`
SELECT
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - next_attempt_at))) FILTER (WHERE status = 'pending'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - next_attempt_at))) FILTER (WHERE status = 'retry'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - sending_started_at))) FILTER (WHERE status = 'sending'), 0)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'sending')`).
		Row().
		Scan(
			&snapshot.OldestPendingAgeSeconds,
			&snapshot.OldestRetryAgeSeconds,
			&snapshot.OldestSendingAgeSeconds,
		)
}

func (s alarmDispatchMaintenanceGormStore) DeleteTerminal(
	ctx context.Context,
	status dispatchoutbox.Status,
	retentionDays int,
	limit int,
) (int64, error) {
	column, ok := alarmDispatchTerminalTimestampColumn(status)
	if !ok {
		return 0, fmt.Errorf("unsupported alarm dispatch retention status: %s", status)
	}
	query := fmt.Sprintf(`
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = ?
      AND %s < NOW() - (?::int * INTERVAL '1 day')
    ORDER BY %s ASC, id ASC
    LIMIT ?
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id`, column, column)
	result := s.db.WithContext(ctx).Exec(query, string(status), retentionDays, clampAlarmDispatchRetentionLimit(limit))
	return result.RowsAffected, result.Error
}

func (s alarmDispatchMaintenanceGormStore) DeleteOrphanEvents(ctx context.Context, retentionDays int, limit int) (int64, error) {
	result := s.db.WithContext(ctx).Exec(`
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - (?::int * INTERVAL '1 day')
      AND NOT EXISTS (
          SELECT 1 FROM alarm_dispatch_deliveries d WHERE d.event_id = e.id
      )
    ORDER BY e.created_at ASC, e.id ASC
    LIMIT ?
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id`, retentionDays, clampAlarmDispatchRetentionLimit(limit))
	return result.RowsAffected, result.Error
}
