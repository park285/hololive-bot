package dispatchrun

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
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
		snapshot: alarmDispatchBacklogSnapshot{QuarantinedRows: 55, OldestQuarantinedAgeSeconds: 86400},
	}))
	require.InDelta(t, 55, testutil.ToFloat64(alarmDispatchPGQuarantinedRows), 0.000001)
	require.InDelta(t, 86400, testutil.ToFloat64(alarmDispatchPGOldestQuarantinedAgeSeconds), 0.000001)
	require.InDelta(t, 1, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess), 0.000001)

	require.Error(t, runner.observeBacklog(t.Context(), quarantineSnapshotObserver{err: errors.New("read failed")}))
	require.InDelta(t, 55, testutil.ToFloat64(alarmDispatchPGQuarantinedRows), 0.000001)
	require.InDelta(t, 86400, testutil.ToFloat64(alarmDispatchPGOldestQuarantinedAgeSeconds), 0.000001)
	require.Zero(t, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess))

	require.NoError(t, runner.observeBacklog(t.Context(), quarantineSnapshotObserver{}))
	require.Zero(t, testutil.ToFloat64(alarmDispatchPGQuarantinedRows))
	require.Zero(t, testutil.ToFloat64(alarmDispatchPGOldestQuarantinedAgeSeconds))
	require.InDelta(t, 1, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess), 0.000001)
}

func TestBacklogSnapshotCountsRetainedQuarantineWithoutChangingActiveBacklog(t *testing.T) {
	pool := dbtest.NewPool(t)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer conn.Release()

	_, err = conn.Exec(t.Context(), `
		CREATE TEMP TABLE alarm_dispatch_deliveries (
			id bigint, status text, next_attempt_at timestamptz,
			sending_started_at timestamptz, quarantined_at timestamptz
		);
		INSERT INTO alarm_dispatch_deliveries VALUES
			(1, 'quarantined', NOW(), NULL, NOW() - INTERVAL '2 days'),
			(2, 'quarantined', NOW(), NULL, NOW() - INTERVAL '1 day'),
			(3, 'sent', NOW(), NULL, NOW() - INTERVAL '3 days'),
			(4, 'pending', NOW() - INTERVAL '1 minute', NULL, NULL),
			(5, 'leased', NOW(), NULL, NULL);
	`)
	require.NoError(t, err)

	store := alarmDispatchMaintenancePgxStore{db: conn}
	snapshot, err := store.BacklogSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), snapshot.QuarantinedRows)
	require.InDelta(t, 2*86400, snapshot.OldestQuarantinedAgeSeconds, 5)
	require.Equal(t, int64(1), snapshot.RowsByStatus[dispatchoutbox.StatusPending])
	require.Equal(t, int64(1), snapshot.RowsByStatus[dispatchoutbox.StatusLeased])
	require.NotContains(t, snapshot.RowsByStatus, dispatchoutbox.StatusQuarantined)

	_, err = conn.Exec(t.Context(), "DELETE FROM alarm_dispatch_deliveries WHERE status = 'quarantined'")
	require.NoError(t, err)

	snapshot, err = store.BacklogSnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.QuarantinedRows)
	require.Zero(t, snapshot.OldestQuarantinedAgeSeconds)
}
