package alarmdispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
)

type quarantineSnapshotObserver struct {
	snapshot alarmDispatchBacklogSnapshot
	err      error
}

func (o quarantineSnapshotObserver) BacklogSnapshot(context.Context) (alarmDispatchBacklogSnapshot, error) {
	return o.snapshot, o.err
}

func TestQuarantineObservationPreservesValuesOnFailureAndClearsOnEmptySuccess(t *testing.T) {
	runner := &alarmDispatchMaintenanceRunner{}
	require.NoError(t, runner.observeBacklog(t.Context(), quarantineSnapshotObserver{
		snapshot: alarmDispatchBacklogSnapshot{
			QuarantinedRows: 55, OldestQuarantinedAgeSeconds: 86400,
			UnreviewedQuarantinedRows: 3, OldestUnreviewedQuarantinedAgeSeconds: 3600,
		},
	}))
	requireQuarantineGauges(t, 55, 86400, 3, 3600)
	require.InDelta(t, 1, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess), 0.000001)

	require.Error(t, runner.observeBacklog(t.Context(), quarantineSnapshotObserver{err: errors.New("read failed")}))
	requireQuarantineGauges(t, 55, 86400, 3, 3600)
	require.Zero(t, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess))

	require.NoError(t, runner.observeBacklog(t.Context(), quarantineSnapshotObserver{}))
	requireQuarantineGauges(t, 0, 0, 0, 0)
	require.InDelta(t, 1, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess), 0.000001)
}

func TestBacklogSnapshotCountsRetainedQuarantineWithoutChangingActiveBacklog(t *testing.T) {
	store, conn := newQuarantineReviewStore(t)

	_, err := conn.Exec(t.Context(), `
		INSERT INTO alarm_dispatch_deliveries (id, status, send_unit_id, quarantined_at) VALUES
			(1, 'quarantined', 10, NOW() - INTERVAL '2 days'),
			(2, 'quarantined', 20, NOW() - INTERVAL '1 day');
		INSERT INTO alarm_dispatch_deliveries (id, status, next_attempt_at) VALUES
			(3, 'sent', NOW()),
			(4, 'pending', NOW() - INTERVAL '1 minute'),
			(5, 'leased', NOW());
	`)
	require.NoError(t, err)

	snapshot, err := store.BacklogSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), snapshot.QuarantinedRows)
	require.InDelta(t, 2*86400, snapshot.OldestQuarantinedAgeSeconds, 5)
	require.Equal(t, int64(2), snapshot.UnreviewedQuarantinedRows)
	require.InDelta(t, 2*86400, snapshot.OldestUnreviewedQuarantinedAgeSeconds, 5)
	require.Equal(t, int64(1), snapshot.RowsByStatus[dispatchoutbox.StatusPending])
	require.Equal(t, int64(1), snapshot.RowsByStatus[dispatchoutbox.StatusLeased])
	require.NotContains(t, snapshot.RowsByStatus, dispatchoutbox.StatusQuarantined)

	_, err = conn.Exec(t.Context(), "DELETE FROM alarm_dispatch_deliveries WHERE status = 'quarantined'")
	require.NoError(t, err)

	snapshot, err = store.BacklogSnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.QuarantinedRows)
	require.Zero(t, snapshot.OldestQuarantinedAgeSeconds)
	require.Zero(t, snapshot.UnreviewedQuarantinedRows)
	require.Zero(t, snapshot.OldestUnreviewedQuarantinedAgeSeconds)
}

// 같은 행 revision의 closeout receipt만 검토 대상에서 빼고, 보존 총량·경과는 유지한다.
func TestBacklogSnapshotExcludesOnlyRowsMatchingCloseoutReceipt(t *testing.T) {
	store, conn := newQuarantineReviewStore(t)
	ctx := t.Context()

	_, err := conn.Exec(ctx, `
		INSERT INTO alarm_dispatch_deliveries (id, status, send_unit_id, attempt_count, quarantined_at, updated_at) VALUES
			(1, 'quarantined', 10, 1, NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days'),
			(2, 'quarantined', 10, 1, NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days');
	`)
	require.NoError(t, err)

	// receipt는 운영 세션의 시간대와 무관하게 같은 시각으로 비교된다.
	recordQuarantineCloseout(t, conn, 10, "Asia/Seoul")

	snapshot, err := store.BacklogSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), snapshot.QuarantinedRows)
	require.InDelta(t, 7*86400, snapshot.OldestQuarantinedAgeSeconds, 5)
	require.Zero(t, snapshot.UnreviewedQuarantinedRows)
	require.Zero(t, snapshot.OldestUnreviewedQuarantinedAgeSeconds)

	// receipt 없는 새 격리는 receipt가 있는 send unit과 섞여도 검토 대상이다.
	_, err = conn.Exec(ctx, `INSERT INTO alarm_dispatch_deliveries (id, status, send_unit_id, attempt_count, quarantined_at, updated_at)
		VALUES (3, 'quarantined', 30, 1, NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day')`)
	require.NoError(t, err)

	snapshot, err = store.BacklogSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), snapshot.QuarantinedRows)
	require.Equal(t, int64(1), snapshot.UnreviewedQuarantinedRows)
	require.InDelta(t, 86400, snapshot.OldestUnreviewedQuarantinedAgeSeconds, 5)

	// 재처리 뒤 다시 격리된 행은 기존 receipt가 있어도 다시 검토 대상이다.
	_, err = conn.Exec(ctx, `UPDATE alarm_dispatch_deliveries
		SET attempt_count = 2, quarantined_at = NOW() - INTERVAL '2 days', updated_at = NOW() - INTERVAL '2 days'
		WHERE id = 2`)
	require.NoError(t, err)

	snapshot, err = store.BacklogSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), snapshot.QuarantinedRows)
	require.InDelta(t, 7*86400, snapshot.OldestQuarantinedAgeSeconds, 5)
	require.Equal(t, int64(2), snapshot.UnreviewedQuarantinedRows)
	require.InDelta(t, 2*86400, snapshot.OldestUnreviewedQuarantinedAgeSeconds, 5)

	// revision만 바뀌어도 기존 결정은 현재 행에 적용되지 않는다.
	_, err = conn.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET updated_at = NOW() WHERE id = 1`)
	require.NoError(t, err)

	snapshot, err = store.BacklogSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), snapshot.UnreviewedQuarantinedRows)
	require.InDelta(t, 7*86400, snapshot.OldestUnreviewedQuarantinedAgeSeconds, 5)
}

// receipt 조회 실패를 검토 완료 0건으로 덮지 않는다.
func TestBacklogSnapshotReceiptFailureKeepsPreviousQuarantineValues(t *testing.T) {
	store, conn := newQuarantineReviewStore(t)
	ctx := t.Context()

	_, err := conn.Exec(ctx, `INSERT INTO alarm_dispatch_deliveries (id, status, send_unit_id, attempt_count, quarantined_at, updated_at)
		VALUES (1, 'quarantined', 10, 1, NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day')`)
	require.NoError(t, err)

	runner := &alarmDispatchMaintenanceRunner{}
	require.NoError(t, runner.observeBacklog(ctx, store))
	requireQuarantineGaugeCounts(t, 1, 1)

	_, err = conn.Exec(ctx, `ALTER TABLE pg_temp.alarm_dispatch_closeout_receipts RENAME COLUMN status_metadata TO hidden_metadata`)
	require.NoError(t, err)

	require.Error(t, runner.observeBacklog(ctx, store))
	requireQuarantineGaugeCounts(t, 1, 1)
	require.Zero(t, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess))
}

func newQuarantineReviewStore(t *testing.T) (alarmDispatchMaintenancePgxStore, *pgxpool.Conn) {
	t.Helper()

	pool := dbtest.NewPool(t)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(conn.Release)

	// 임시 테이블이 같은 이름의 public 테이블을 가려 조회 SQL을 그대로 실행한다.
	//nolint:misspell // cancelled_at은 변경할 수 없는 정본 DB 열 이름입니다.
	_, err = conn.Exec(t.Context(), `
		CREATE TEMP TABLE alarm_dispatch_deliveries (
			id bigint, status text, send_unit_id bigint, attempt_count integer NOT NULL DEFAULT 0,
			next_attempt_at timestamptz NOT NULL DEFAULT NOW(), sending_started_at timestamptz,
			quarantined_at timestamptz, sent_at timestamptz, cancelled_at timestamptz,
			updated_at timestamptz NOT NULL DEFAULT NOW()
		);
		CREATE TEMP TABLE alarm_dispatch_closeout_receipts (
			send_unit_id bigint, target_ids bigint[], status_metadata jsonb
		);
	`)
	require.NoError(t, err)

	return alarmDispatchMaintenancePgxStore{db: conn}, conn
}

// recordQuarantineCloseout는 migration 228 snapshot과 같은 status_metadata 모양으로 receipt를 남긴다.
func recordQuarantineCloseout(t *testing.T, conn *pgxpool.Conn, sendUnitID int64, timeZone string) {
	t.Helper()

	ctx := t.Context()
	_, err := conn.Exec(ctx, "SELECT set_config('TimeZone', $1, false)", timeZone)
	require.NoError(t, err)

	//nolint:misspell // cancelledAt·cancelled_at은 migration 228 영수증·DB 식별자 계약입니다.
	_, err = conn.Exec(ctx, `
		INSERT INTO alarm_dispatch_closeout_receipts (send_unit_id, target_ids, status_metadata)
		SELECT $1::bigint, array_agg(id ORDER BY id), jsonb_agg(jsonb_build_object(
			'id', id::text, 'status', status, 'attemptCount', attempt_count, 'lastErrorCode', '',
			'updatedAt', updated_at, 'quarantinedAt', quarantined_at, 'sentAt', sent_at, 'cancelledAt', cancelled_at
		) ORDER BY id)
		FROM alarm_dispatch_deliveries WHERE send_unit_id = $1::bigint`, sendUnitID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "SELECT set_config('TimeZone', 'UTC', false)")
	require.NoError(t, err)
}

func requireQuarantineGauges(t *testing.T, rows, age, unreviewed, unreviewedAge float64) {
	t.Helper()
	requireQuarantineGaugeCounts(t, rows, unreviewed)
	require.InDelta(t, age, testutil.ToFloat64(alarmDispatchPGOldestQuarantinedAgeSeconds), 0.000001)
	require.InDelta(t, unreviewedAge, testutil.ToFloat64(alarmDispatchPGOldestUnreviewedAgeSeconds), 0.000001)
}

func requireQuarantineGaugeCounts(t *testing.T, rows, unreviewed float64) {
	t.Helper()
	require.InDelta(t, rows, testutil.ToFloat64(alarmDispatchPGQuarantinedRows), 0.000001)
	require.InDelta(t, unreviewed, testutil.ToFloat64(alarmDispatchPGUnreviewedQuarantinedRows), 0.000001)
}
