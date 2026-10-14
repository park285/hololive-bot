package alarmdispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

const alarmDispatchBacklogObservationFailedMetric = "alarm_dispatch_pg_backlog_observation_failed_total"

// 관측 실패가 Start 오류가 되면 형제 egress runner가 취소되므로, 실패해도 다음 주기에 다시 조회해야 한다.
func TestAlarmDispatchBacklogObserverKeepsObservingAfterFailuresUntilCancelled(t *testing.T) {
	store := &alarmDispatchBacklogObserverTestStore{err: errors.New("read failed")}
	observer := &alarmDispatchBacklogObserver{
		store:        store,
		interval:     5 * time.Millisecond,
		queryTimeout: time.Second,
		logger:       slog.New(slog.DiscardHandler),
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- observer.Start(ctx) }()

	require.Eventually(t, func() bool { return store.calls.Load() >= 3 }, 5*time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("backlog observer did not stop after context cancellation")
	}
}

func TestAlarmDispatchBacklogObserverFailureKeepsLastValuesAndLowersSuccess(t *testing.T) {
	var logs bytes.Buffer

	store := &alarmDispatchBacklogObserverTestStore{snapshot: alarmDispatchBacklogSnapshot{
		RowsByStatus:            map[dispatchoutbox.Status]int64{dispatchoutbox.StatusPending: 7},
		OldestPendingAgeSeconds: 70,
	}}
	observer := &alarmDispatchBacklogObserver{
		store:        store,
		queryTimeout: 20 * time.Millisecond,
		logger:       slog.New(slog.NewTextHandler(&logs, nil)),
	}

	observer.observeOnce(t.Context())
	require.True(t, store.sawDeadline.Load())
	require.InDelta(t, 70, testutil.ToFloat64(alarmDispatchPGOldestPendingAgeSeconds), 0.000001)
	require.InDelta(t, 7, testutil.ToFloat64(alarmDispatchPGBacklogRows.WithLabelValues(string(dispatchoutbox.StatusPending))), 0.000001)
	require.InDelta(t, 1, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess), 0.000001)
	require.Empty(t, logs.String())

	lastSuccess := testutil.ToFloat64(alarmDispatchPGBacklogSnapshotLastSuccess)

	tests := []struct {
		name           string
		err            error
		waitForTimeout bool
	}{
		{name: "immediate error", err: errors.New("read failed")},
		{name: "query timeout", waitForTimeout: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store.err = tt.err
			store.waitForTimeout = tt.waitForTimeout

			before := alarmDispatchCounterMetricValue(t, alarmDispatchBacklogObservationFailedMetric)

			observer.observeOnce(t.Context())

			assert.InDelta(t, before+1, alarmDispatchCounterMetricValue(t, alarmDispatchBacklogObservationFailedMetric), 0)
			assert.Zero(t, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotSuccess))
			assert.InDelta(t, 70, testutil.ToFloat64(alarmDispatchPGOldestPendingAgeSeconds), 0.000001)
			assert.InDelta(t, 7, testutil.ToFloat64(alarmDispatchPGBacklogRows.WithLabelValues(string(dispatchoutbox.StatusPending))), 0.000001)
			assert.InDelta(t, lastSuccess, testutil.ToFloat64(alarmDispatchPGBacklogSnapshotLastSuccess), 0)
			assert.Contains(t, logs.String(), "alarm dispatch backlog observation failed")
		})
	}
}

func TestAlarmDispatchBacklogObserverParentCancellationIsNotCountedAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &alarmDispatchBacklogObserverTestStore{cancelParent: cancel, waitForTimeout: true}

	var logs bytes.Buffer

	observer := &alarmDispatchBacklogObserver{
		store:        store,
		interval:     time.Hour,
		queryTimeout: time.Second,
		logger:       slog.New(slog.NewTextHandler(&logs, nil)),
	}
	before := alarmDispatchCounterMetricValue(t, alarmDispatchBacklogObservationFailedMetric)

	require.NoError(t, observer.Start(ctx))
	assert.EqualValues(t, 1, store.calls.Load())
	assert.InDelta(t, before, alarmDispatchCounterMetricValue(t, alarmDispatchBacklogObservationFailedMetric), 0)
	assert.Empty(t, logs.String())
}

type alarmDispatchBacklogObserverTestStore struct {
	snapshot       alarmDispatchBacklogSnapshot
	err            error
	waitForTimeout bool
	cancelParent   context.CancelFunc
	calls          atomic.Int64
	sawDeadline    atomic.Bool
}

func (s *alarmDispatchBacklogObserverTestStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	s.calls.Add(1)

	_, hasDeadline := ctx.Deadline()
	s.sawDeadline.Store(hasDeadline)

	if s.cancelParent != nil {
		s.cancelParent()
	}

	if s.waitForTimeout {
		<-ctx.Done()

		return alarmDispatchBacklogSnapshot{}, fmt.Errorf("blocked backlog query: %w", ctx.Err())
	}

	if s.err != nil {
		return alarmDispatchBacklogSnapshot{}, s.err
	}

	return s.snapshot, nil
}
