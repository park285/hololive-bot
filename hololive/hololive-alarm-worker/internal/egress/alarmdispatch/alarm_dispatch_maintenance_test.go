package alarmdispatch

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

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

func TestAlarmDispatchMaintenanceSkipsRetentionWhenAdvisoryLockUnavailable(t *testing.T) {
	store := &alarmDispatchMaintenanceTestStore{locked: false}
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
	assert.Equal(t, 1, store.deletedSendUnits)
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
	store := &alarmDispatchMaintenanceTestStore{locked: true}
	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		retentionEnabled: true,
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
		sentDays:         90,
		dlqDays:          180,
		quarantinedDays:  180,
		cancelledDays:    90,
		eventDays:        90,
		retentionLockKey: 42,
	}

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	assert.ErrorIs(t, err, deleteErr)
}

func TestAlarmDispatchMaintenanceParentCancellationIsNotCountedAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &alarmDispatchMaintenanceTestStore{
		locked:     true,
		cancelLock: cancel,
	}

	var logs bytes.Buffer

	runner := &alarmDispatchMaintenanceRunner{
		store:            store,
		retentionEnabled: true,
		queryTimeout:     time.Second,
		interval:         time.Hour,
		logger:           slog.New(slog.NewTextHandler(&logs, nil)),
	}
	beforeRetention := alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_retention_failed_total")

	err := runner.Start(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, store.lockCalls)
	assert.Empty(t, store.deletedTerminal)
	assert.InDelta(t, beforeRetention, alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_retention_failed_total"), 0)
	assert.Empty(t, logs.String())
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
	cancelLock         context.CancelFunc
	sawDeadline        bool
}

func (s *alarmDispatchMaintenanceTestStore) WithAdvisoryLock(
	ctx context.Context,
	_ int64,
	fn func(context.Context, alarmDispatchMaintenanceDataStore) error,
) error {
	s.lockCalls++

	_, s.sawDeadline = ctx.Deadline()

	if s.cancelLock != nil {
		s.cancelLock()

		return fmt.Errorf("lock wait: %w", ctx.Err())
	}

	if !s.locked {
		return nil
	}

	if err := fn(ctx, s); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteTerminal(_ context.Context, status dispatchoutbox.Status, _, _ int) (int64, error) {
	if s.deleteErr != nil {
		return 0, s.deleteErr
	}

	if !alarmDispatchMaintenanceStatusIsDeletable(status) {
		return 0, errors.New("active status delete requested")
	}

	s.deletedTerminal = append(s.deletedTerminal, status)

	return s.deleteTerminalRows[status], nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteOrphanEvents(context.Context, int, int) (alarmDispatchOrphanEventCounts, error) {
	s.deletedEvents++
	return alarmDispatchOrphanEventCounts{events: s.deleteEventRows}, nil
}

func (s *alarmDispatchMaintenanceTestStore) DeleteOrphanSendUnits(context.Context, int) (int64, error) {
	s.deletedSendUnits++
	return 0, nil
}

func alarmDispatchMaintenanceStatusIsDeletable(status dispatchoutbox.Status) bool {
	_, ok := alarmDispatchRetentionDeleteSQL[status]
	return ok
}
