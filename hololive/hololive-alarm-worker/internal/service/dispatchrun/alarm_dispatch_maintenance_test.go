package dispatchrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

func TestAlarmDispatchMaintenanceSkipsRetentionWhenAdvisoryLockUnavailable(t *testing.T) {
	store := &alarmDispatchMaintenanceTestStore{locked: false}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		limit:            1000,
		retentionLockKey: 42,
	}

	require.NoError(t, runner.RunOnce(t.Context()))
	assert.Equal(t, 1, store.lockCalls)
	assert.Empty(t, store.deletedTerminal)
	assert.Zero(t, store.deletedSendUnits)
	assert.Zero(t, store.deletedEvents)
}

func TestAlarmDispatchMaintenanceDeletesTerminalRowsAndOrphanEventsInChunks(t *testing.T) {
	store := &alarmDispatchMaintenanceTestStore{
		locked: true,
		deleteTerminalRows: map[dispatchoutbox.Status]int64{
			dispatchoutbox.StatusSent:        3,
			dispatchoutbox.StatusQuarantined: 2,
		},
		deleteEventRows: 4,
	}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		limit:            1000,
		sentDays:         90,
		dlqDays:          180,
		quarantinedDays:  180,
		cancelledDays:    90,
		eventDays:        90,
		retentionLockKey: 42,
	}

	require.NoError(t, runner.RunOnce(t.Context()))
	assert.Equal(t, []dispatchoutbox.Status{
		dispatchoutbox.StatusShadowed,
		dispatchoutbox.StatusSent,
		dispatchoutbox.StatusDLQ,
		dispatchoutbox.StatusQuarantined,
		dispatchoutbox.StatusCancelled,
	}, store.deletedTerminal)
	assert.Equal(t, 1, store.deletedSendUnits)
	assert.Equal(t, 1, store.deletedEvents)
}

func TestAlarmDispatchMaintenanceDoesNotDeleteActiveStatuses(t *testing.T) {
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusPending))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusRetry))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusLeased))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusSending))
	assert.True(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusSent))
	assert.True(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusShadowed))
}

func TestAlarmDispatchMaintenanceClampsRetentionLimit(t *testing.T) {
	assert.Equal(t, 1000, clampAlarmDispatchRetentionLimit(0))
	assert.Equal(t, alarmDispatchRetentionMaxLimit, clampAlarmDispatchRetentionLimit(alarmDispatchRetentionMaxLimit+1))
	assert.Equal(t, 500, clampAlarmDispatchRetentionLimit(500))
}

func TestAlarmDispatchMaintenanceUsesQueryTimeout(t *testing.T) {
	store := &alarmDispatchMaintenanceTestStore{locked: true, expectDeadline: true}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
		retentionEnabled: false,
		queryTimeout:     time.Second,
		limit:            1000,
	}

	require.NoError(t, runner.RunOnce(t.Context()))
	assert.True(t, store.sawDeadline)
}

func TestAlarmDispatchMaintenanceReturnsRetentionDeleteErrors(t *testing.T) {
	deleteErr := errors.New("delete failed")
	store := &alarmDispatchMaintenanceTestStore{locked: true, deleteErr: deleteErr}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		limit:            1000,
		retentionLockKey: 42,
	}

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	assert.ErrorIs(t, err, deleteErr)
}

func TestAlarmDispatchMaintenanceObservationFailuresDoNotBlockDeletion(t *testing.T) {
	tests := []struct {
		name           string
		observationErr error
		waitForTimeout bool
	}{
		{name: "immediate error", observationErr: errors.New("observe failed")},
		{name: "timeout", waitForTimeout: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &alarmDispatchMaintenanceTestStore{
				locked:         true,
				observationErr: tt.observationErr,
				waitForTimeout: tt.waitForTimeout,
			}
			runner := &alarmDispatchMaintenanceRunner{
				store:            store,
				observerStore:    store,
				retentionEnabled: true,
				queryTimeout:     20 * time.Millisecond,
				limit:            1000,
				retentionLockKey: 42,
			}

			require.NotPanics(t, func() {
				require.NoError(t, runner.RunOnce(t.Context()))
			})
			assert.Len(t, store.deletedTerminal, 5)
			assert.Equal(t, 1, store.deletedSendUnits)
			assert.Equal(t, 1, store.deletedEvents)
			require.NotNil(t, store.observationDone)
			require.NotNil(t, store.deletionDone)
			assert.NotEqual(t, store.observationDone, store.deletionDone)
			assert.NoError(t, store.deletionCtxErr)
		})
	}
}

func TestAlarmDispatchMaintenanceParentCancellationIsNotCountedAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &alarmDispatchMaintenanceTestStore{
		locked:            true,
		cancelObservation: cancel,
		waitForTimeout:    true,
	}

	var logs bytes.Buffer

	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		observerStore:    store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		logger:           slog.New(slog.NewTextHandler(&logs, nil)),
	}
	beforeObservation := alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_backlog_observation_failed_total")
	beforeRetention := alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_retention_failed_total")

	err := runner.Start(ctx)

	require.NoError(t, err)
	assert.Zero(t, store.lockCalls)
	assert.Empty(t, store.deletedTerminal)
	assert.Zero(t, store.deletedSendUnits)
	assert.Zero(t, store.deletedEvents)
	assert.InDelta(t, beforeObservation, alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_backlog_observation_failed_total"), 0)
	assert.InDelta(t, beforeRetention, alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_retention_failed_total"), 0)
	assert.Empty(t, logs.String())
}

func TestAlarmDispatchMaintenanceNilStoreReturnsWithoutObservation(t *testing.T) {
	observer := &alarmDispatchMaintenanceTestStore{observationErr: errors.New("must not run")}
	runner := &alarmDispatchMaintenanceRunner{observerStore: observer}

	require.NoError(t, runner.RunOnce(t.Context()))
	assert.Zero(t, observer.observationCalls)
}

type alarmDispatchMaintenanceTestStore struct {
	locked             bool
	lockCalls          int
	deletedTerminal    []dispatchoutbox.Status
	deletedSendUnits   int
	deletedEvents      int
	deleteTerminalRows map[dispatchoutbox.Status]int64
	deleteEventRows    int64
	deleteErr          error
	observationErr     error
	waitForTimeout     bool
	cancelObservation  context.CancelFunc
	observationCalls   int
	expectDeadline     bool
	sawDeadline        bool
	observationDone    <-chan struct{}
	deletionDone       <-chan struct{}
	deletionCtxErr     error
}

func (s *alarmDispatchMaintenanceTestStore) WithAdvisoryLock(
	ctx context.Context,
	_ int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	s.lockCalls++
	if !s.locked {
		return nil
	}

	if err := fn(ctx, s); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func (s *alarmDispatchMaintenanceTestStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	s.observationCalls++

	s.observationDone = ctx.Done()

	if s.expectDeadline {
		_, s.sawDeadline = ctx.Deadline()
	}

	if s.cancelObservation != nil {
		s.cancelObservation()
	}

	if s.waitForTimeout {
		<-ctx.Done()

		if err := ctx.Err(); err != nil {
			return alarmDispatchBacklogSnapshot{}, fmt.Errorf("blocked backlog query: %w", err)
		}

		return alarmDispatchBacklogSnapshot{}, nil
	}

	if s.observationErr != nil {
		return alarmDispatchBacklogSnapshot{}, s.observationErr
	}

	return alarmDispatchBacklogSnapshot{
		RowsByStatus: map[dispatchoutbox.Status]int64{
			dispatchoutbox.StatusPending: 1,
			dispatchoutbox.StatusRetry:   2,
		},
		OldestPendingAgeSeconds: 3,
		OldestRetryAgeSeconds:   4,
		OldestSendingAgeSeconds: 5,
	}, nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteTerminal(ctx context.Context, status dispatchoutbox.Status, _, _ int) (int64, error) {
	s.deletionDone = ctx.Done()
	s.deletionCtxErr = ctx.Err()

	if s.deleteErr != nil {
		return 0, s.deleteErr
	}

	if !alarmDispatchMaintenanceStatusIsDeletable(status) {
		return 0, errors.New("active status delete requested")
	}

	s.deletedTerminal = append(s.deletedTerminal, status)

	return s.deleteTerminalRows[status], nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteOrphanEvents(context.Context, int, int) (int64, error) {
	s.deletedEvents++
	return s.deleteEventRows, nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteOrphanSendUnits(context.Context, int) (int64, error) {
	s.deletedSendUnits++
	return 0, nil
}

func alarmDispatchMaintenanceStatusIsDeletable(status dispatchoutbox.Status) bool {
	_, ok := alarmDispatchTerminalTimestampColumn(status)
	return ok
}
