package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlarmDispatchMaintenanceSkipsRetentionWhenAdvisoryLockUnavailable(t *testing.T) {
	store := &alarmDispatchMaintenanceTestStore{locked: false}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		limit:            1000,
		retentionLockKey: 42,
	}

	require.NoError(t, runner.RunOnce(t.Context()))
	assert.Equal(t, 1, store.lockCalls)
	assert.Empty(t, store.deletedTerminal)
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
		dispatchoutbox.StatusSent,
		dispatchoutbox.StatusDLQ,
		dispatchoutbox.StatusQuarantined,
		dispatchoutbox.StatusCancelled,
	}, store.deletedTerminal)
	assert.Equal(t, 1, store.deletedEvents)
}

func TestAlarmDispatchMaintenanceDoesNotDeleteActiveStatuses(t *testing.T) {
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusPending))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusRetry))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusLeased))
	assert.False(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusSending))
	assert.True(t, alarmDispatchMaintenanceStatusIsDeletable(dispatchoutbox.StatusSent))
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
		retentionEnabled: true,
		queryTimeout:     time.Second,
		limit:            1000,
		retentionLockKey: 42,
	}

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	assert.ErrorIs(t, err, deleteErr)
}

type alarmDispatchMaintenanceTestStore struct {
	locked             bool
	lockCalls          int
	deletedTerminal    []dispatchoutbox.Status
	deletedEvents      int
	deleteTerminalRows map[dispatchoutbox.Status]int64
	deleteEventRows    int64
	deleteErr          error
	expectDeadline     bool
	sawDeadline        bool
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
	return fn(ctx, s)
}

func (s *alarmDispatchMaintenanceTestStore) BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error) {
	if s.expectDeadline {
		_, s.sawDeadline = ctx.Deadline()
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

func (s *alarmDispatchMaintenanceTestStore) DeleteTerminal(_ context.Context, status dispatchoutbox.Status, _ int, _ int) (int64, error) {
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

func alarmDispatchMaintenanceStatusIsDeletable(status dispatchoutbox.Status) bool {
	_, ok := alarmDispatchTerminalTimestampColumn(status)
	return ok
}
