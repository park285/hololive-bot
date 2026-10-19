package notificationdelivery

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

func TestCleanupContinuesPastRearmedFullBatch(t *testing.T) {
	repo := testRepository(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	require.NoError(t, repo.EnqueueBatch(ctx, buildOutboxBatchItems(2)))

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET status='FAILED',
		created_at=clock_timestamp()-interval '90 days'+id*interval '1 second'`)
	require.NoError(t, err)

	upsertSQL, err := os.ReadFile("../../../../hololive-shared/pkg/service/delivery/queries/outbox_enqueue_batch_upsert.sql")
	require.NoError(t, err)

	tx, err := repo.pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackTestTx(t.Context(), t, tx)

	_, err = tx.Exec(ctx, string(upsertSQL), `[{"kind":"MEMBER_NEWS_WEEKLY","period_key":"2026-W08","room_id":"room-batch-0","content_id":"2026-W08:room-batch-0","payload":{"message":"replacement"}}]`)
	require.NoError(t, err)

	type result struct {
		deleted int64
		err     error
	}

	done := make(chan result, 1)

	go func() {
		deleted, cleanupErr := repo.cleanupInBatches(ctx, time.Now().Add(-30*24*time.Hour), 1)
		done <- result{deleted, cleanupErr}
	}()

	waitForOutboxQueryLock(ctx, t, repo, tx)
	require.NoError(t, tx.Commit(ctx))

	got := <-done
	require.NoError(t, got.err)
	require.EqualValues(t, 1, got.deleted, "skipping a rearmed full batch must not hide the next expired row")

	var pending, terminal int

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='PENDING'),
		count(*) FILTER(WHERE status IN ('SENT','FAILED','QUARANTINED')) FROM notification_delivery_outbox`).Scan(&pending, &terminal))
	require.Equal(t, 1, pending)
	require.Zero(t, terminal)
}

func TestCleanupPreservesCommittedCountOnLaterBatchFailure(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.EnqueueBatch(ctx, buildOutboxBatchItems(2)))

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET status='FAILED',
		created_at=clock_timestamp()-interval '90 days'+id*interval '1 second';
		CREATE FUNCTION reject_later_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.room_id='room-batch-1' THEN RAISE EXCEPTION 'later cleanup batch rejected'; END IF;
			RETURN OLD;
		END $$;
		CREATE TRIGGER reject_later_cleanup BEFORE DELETE ON notification_delivery_outbox
		FOR EACH ROW EXECUTE FUNCTION reject_later_cleanup()`)
	require.NoError(t, err)

	deleted, err := repo.cleanupInBatches(ctx, time.Now().Add(-30*24*time.Hour), 1)
	require.Error(t, err)
	require.EqualValues(t, 1, deleted)

	var roomID string

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT room_id FROM notification_delivery_outbox`).Scan(&roomID))
	require.Equal(t, "room-batch-1", roomID)
}

func TestCleanupCancellationPreservesCommittedBatches(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.EnqueueBatch(ctx, buildOutboxBatchItems(3)))

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET status='FAILED',
		created_at=statement_timestamp()-CASE WHEN room_id='room-batch-2' THEN interval '89 days' ELSE interval '90 days' END`)
	require.NoError(t, err)

	holder, err := repo.pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackTestTx(ctx, t, holder)

	var heldID int64

	require.NoError(t, holder.QueryRow(ctx, `SELECT id FROM notification_delivery_outbox WHERE room_id='room-batch-2' FOR UPDATE`).Scan(&heldID))

	cleanupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		deleted int64
		err     error
	}

	done := make(chan result, 1)

	go func() {
		deleted, cleanupErr := repo.cleanupInBatches(cleanupCtx, time.Now().Add(-30*24*time.Hour), 2)
		done <- result{deleted, cleanupErr}
	}()

	// 먼저 커밋한 2개 뒤의 셋째 행에서 대기한 것을 확인한 뒤 취소한다.
	waitForOutboxQueryLock(ctx, t, repo, holder)

	var cleanupPID int32

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity
		WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid))`,
		holder.Conn().PgConn().PID()).Scan(&cleanupPID))
	cancel()

	got := <-done
	require.ErrorIs(t, got.err, context.Canceled)
	require.EqualValues(t, 2, got.deleted)
	// client의 취소 반환은 server 중단 확인이 아니다. holder를 풀기 전에 실제 중단을 확인한다.
	waitForCleanupBackendStop(ctx, t, repo, cleanupPID)
	require.NoError(t, pgxdb.Rollback(ctx, holder))

	var remainingID int64

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT id FROM notification_delivery_outbox`).Scan(&remainingID))
	require.Equal(t, heldID, remainingID)
}

func waitForCleanupBackendStop(ctx context.Context, t *testing.T, repo *outboxFixture, pid int32) {
	t.Helper()

	require.Eventually(t, func() bool {
		var stopped bool

		err := repo.pool.QueryRow(ctx, `SELECT NOT EXISTS(
			SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='active')`, pid).Scan(&stopped)

		return err == nil && stopped
	}, 2*time.Second, 5*time.Millisecond)
}
