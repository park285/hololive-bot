package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func TestMarkSentBatchDoesNotBypassActiveLease(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W31", "lease-protected-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}

	if err := repository.MarkSentBatch(ctx, []int64{items[0].ID}); err != nil {
		t.Fatalf("mark sent batch: %v", err)
	}
	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 1 {
		t.Fatalf("pending = %d, want 1", pending)
	}
	if owner := lockedByOf(t, repository, ctx, items[0].ID); owner == nil || *owner != testWorkerA {
		t.Fatalf("locked_by = %v, want %q", owner, testWorkerA)
	}
}

func TestMarkFailedBatchDoesNotBypassActiveLease(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W32", "lease-protected-failure-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}

	if err := repository.MarkFailedBatch(ctx, []int64{items[0].ID}, "must not bypass lease"); err != nil {
		t.Fatalf("mark failed batch: %v", err)
	}
	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 1 {
		t.Fatalf("pending = %d, want 1", pending)
	}
	if owner := lockedByOf(t, repository, ctx, items[0].ID); owner == nil || *owner != testWorkerA {
		t.Fatalf("locked_by = %v, want %q", owner, testWorkerA)
	}
}

func TestMarkSendingChecksLeaseAtDatabaseExecutionTime(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W33", "db-clock-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items, err := repository.FetchAndLock(ctx, testWorkerA, 1, testLockTTL, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("fetch and lock: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}
	var lockExpiresAt time.Time
	if err := repository.pool.QueryRow(ctx, "SELECT lock_expires_at FROM notification_delivery_outbox WHERE id = $1", items[0].ID).Scan(&lockExpiresAt); err != nil {
		t.Fatalf("load lock expiry: %v", err)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row lock: %v", err)
	}
	defer rollbackTestTx(t, ctx, tx)
	if _, err := tx.Exec(ctx, "SELECT id FROM notification_delivery_outbox WHERE id = $1 FOR UPDATE", items[0].ID); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	type result struct {
		ok  bool
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		ok, markErr := repository.MarkSending(ctx, items[0].ID, testWorkerA, testLease)
		resultCh <- result{ok: ok, err: markErr}
	}()

	waitForOutboxQueryLock(t, repository, ctx, tx)
	waitForOutboxDatabaseTime(t, repository, ctx, lockExpiresAt)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release row lock: %v", err)
	}
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("mark sending: %v", got.err)
	}
	if got.ok {
		t.Fatal("lease that expired while waiting for the database row lock must be fenced")
	}
}

func TestMarkSentRecordsDatabaseExecutionTime(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W34", "db-clock-sent-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row lock: %v", err)
	}
	defer rollbackTestTx(t, ctx, tx)
	if _, err := tx.Exec(ctx, "SELECT id FROM notification_delivery_outbox WHERE id = $1 FOR UPDATE", items[0].ID); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	type result struct {
		ok  bool
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		ok, markErr := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time)
		resultCh <- result{ok: ok, err: markErr}
	}()

	time.Sleep(250 * time.Millisecond)
	releasedAt := time.Now()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release row lock: %v", err)
	}
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("mark sent: %v", got.err)
	}
	if !got.ok {
		t.Fatal("mark sent was fenced unexpectedly")
	}

	var sentAt time.Time
	if err := repository.pool.QueryRow(ctx, "SELECT sent_at FROM notification_delivery_outbox WHERE id = $1", items[0].ID).Scan(&sentAt); err != nil {
		t.Fatalf("load sent_at: %v", err)
	}
	if earliest := releasedAt.Add(-100 * time.Millisecond); sentAt.Before(earliest) {
		t.Fatalf("sent_at = %s, want database execution time at or after %s", sentAt, earliest)
	}
}

func TestMarkFailedSchedulesRetryFromDatabaseExecutionTime(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W35", "db-clock-retry-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row lock: %v", err)
	}
	defer rollbackTestTx(t, ctx, tx)
	if _, err := tx.Exec(ctx, "SELECT id FROM notification_delivery_outbox WHERE id = $1 FOR UPDATE", items[0].ID); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	type result struct {
		ok  bool
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		ok, markErr := repository.MarkFailed(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time, 3, time.Second, "retry")
		resultCh <- result{ok: ok, err: markErr}
	}()

	time.Sleep(250 * time.Millisecond)
	releasedAt := time.Now()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release row lock: %v", err)
	}
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("mark failed: %v", got.err)
	}
	if !got.ok {
		t.Fatal("mark failed was fenced unexpectedly")
	}

	var nextAttemptAt time.Time
	if err := repository.pool.QueryRow(ctx, "SELECT next_attempt_at FROM notification_delivery_outbox WHERE id = $1", items[0].ID).Scan(&nextAttemptAt); err != nil {
		t.Fatalf("load next_attempt_at: %v", err)
	}
	if earliest := releasedAt.Add(900 * time.Millisecond); nextAttemptAt.Before(earliest) {
		t.Fatalf("next_attempt_at = %s, want database execution time plus backoff at or after %s", nextAttemptAt, earliest)
	}
}

func TestFetchAndLockRoundsPositiveSubMillisecondLeaseUp(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W36", "submillisecond-lease-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items, err := repository.FetchAndLock(ctx, testWorkerA, 1, testLockTTL, 500*time.Microsecond)
	if err != nil {
		t.Fatalf("fetch and lock: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}

	var leaseMicroseconds int64
	if err := repository.pool.QueryRow(ctx, `
		SELECT (EXTRACT(EPOCH FROM (lock_expires_at - locked_at)) * 1000000)::bigint
		FROM notification_delivery_outbox
		WHERE id = $1
	`, items[0].ID).Scan(&leaseMicroseconds); err != nil {
		t.Fatalf("load lease duration: %v", err)
	}
	if leaseMicroseconds < 1000 {
		t.Fatalf("lease duration = %dµs, want at least 1000µs", leaseMicroseconds)
	}
}

func TestMarkSentBatchRecordsDatabaseExecutionTime(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W37", "db-clock-batch-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id := onlyRowID(t, repository, ctx)

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row lock: %v", err)
	}
	defer rollbackTestTx(t, ctx, tx)
	if _, err := tx.Exec(ctx, "SELECT id FROM notification_delivery_outbox WHERE id = $1 FOR UPDATE", id); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- repository.MarkSentBatch(ctx, []int64{id})
	}()

	waitForOutboxQueryLock(t, repository, ctx, tx)
	releasedAt := time.Now()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release row lock: %v", err)
	}
	if err := <-resultCh; err != nil {
		t.Fatalf("mark sent batch: %v", err)
	}

	var sentAt time.Time
	if err := repository.pool.QueryRow(ctx, "SELECT sent_at FROM notification_delivery_outbox WHERE id = $1", id).Scan(&sentAt); err != nil {
		t.Fatalf("load sent_at: %v", err)
	}
	if earliest := releasedAt.Add(-100 * time.Millisecond); sentAt.Before(earliest) {
		t.Fatalf("sent_at = %s, want database execution time at or after %s", sentAt, earliest)
	}
}

func TestQuarantineStaleSendingUsesDatabaseExecutionTime(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W38", "db-clock-quarantine-room", "message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) != 1 {
		t.Fatalf("fetch len = %d, want 1", len(items))
	}
	markOutboxSending(t, repository, ctx, &items[0])
	var staleAt time.Time
	if err := repository.pool.QueryRow(ctx, "SELECT sending_started_at + INTERVAL '100 milliseconds' FROM notification_delivery_outbox WHERE id = $1", items[0].ID).Scan(&staleAt); err != nil {
		t.Fatalf("load stale threshold: %v", err)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin table lock: %v", err)
	}
	defer rollbackTestTx(t, ctx, tx)
	if _, err := tx.Exec(ctx, "LOCK TABLE notification_delivery_outbox IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock table: %v", err)
	}

	type result struct {
		count int64
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		count, quarantineErr := repository.QuarantineStaleSending(ctx, 100*time.Millisecond, 1)
		resultCh <- result{count: count, err: quarantineErr}
	}()

	waitForOutboxQueryLock(t, repository, ctx, tx)
	waitForOutboxDatabaseTime(t, repository, ctx, staleAt)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release table lock: %v", err)
	}
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("quarantine stale sending: %v", got.err)
	}
	if got.count != 1 {
		t.Fatalf("quarantined = %d, want 1", got.count)
	}
	if quarantined := countByStatus(t, repository, ctx, deliveryStatusQuarantined); quarantined != 1 {
		t.Fatalf("quarantined status count = %d, want 1", quarantined)
	}
}

func waitForOutboxQueryLock(t *testing.T, repository *OutboxRepository, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	var blockerPID int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatalf("load blocker pid: %v", err)
	}
	require.Eventually(t, func() bool {
		var waiting bool
		err := repository.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
					AND pid <> pg_backend_pid()
					AND $1 = ANY(pg_blocking_pids(pid))
					AND wait_event_type = 'Lock'
					AND query LIKE '%notification_delivery_outbox%'
			)
		`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 5*time.Millisecond)
}

func waitForOutboxDatabaseTime(t *testing.T, repository *OutboxRepository, ctx context.Context, threshold time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		var reached bool
		err := repository.pool.QueryRow(ctx, "SELECT clock_timestamp() >= $1", threshold).Scan(&reached)
		return err == nil && reached
	}, 2*time.Second, 5*time.Millisecond)
}

func rollbackTestTx(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback transaction: %v", err)
	}
}
