package workerruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationEgressRunnerStartsAllActiveRunners(t *testing.T) {
	var firstStarts, secondStarts atomic.Int32
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "first", Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
			firstStarts.Add(1)
			return nil
		})},
		{Name: "second", Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
			secondStarts.Add(1)
			return nil
		})},
	}}

	require.NoError(t, runner.Start(t.Context()))

	assert.Equal(t, int32(1), firstStarts.Load())
	assert.Equal(t, int32(1), secondStarts.Load())
}

func TestNotificationEgressRunnerSkipsNilSchedulers(t *testing.T) {
	var starts atomic.Int32
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "nil-runner", Scheduler: nil},
		{Name: "active", Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
			starts.Add(1)
			return nil
		})},
	}}

	require.NoError(t, runner.Start(t.Context()))

	assert.Equal(t, int32(1), starts.Load())
}

func TestNotificationEgressRunnerReturnsNilWhenNoActiveRunners(t *testing.T) {
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "nil-runner", Scheduler: nil},
	}}

	done := make(chan error, 1)
	go func() { done <- runner.Start(t.Context()) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start blocked with no active runners")
	}
}

func TestNotificationEgressRunnerReturnsNilOnContextCancel(t *testing.T) {
	started := make(chan struct{})
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "long-running", Scheduler: runtimeAlarmSchedulerFunc(func(runnerCtx context.Context) error {
			close(started)
			<-runnerCtx.Done()
			return nil
		})},
	}}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runner.Start(ctx) }()

	<-started
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestNotificationEgressRunnerWrapsRunnerFailure(t *testing.T) {
	sentinel := errors.New("runner boom")
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "failing", Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error { return sentinel })},
	}}

	err := runner.Start(t.Context())

	if err == nil {
		t.Fatal("Start() error = nil, want wrapped runner failure")
	}
	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "notification egress runner stopped")
}

type runtimeAlarmSchedulerFunc func(context.Context) error

func (f runtimeAlarmSchedulerFunc) Start(ctx context.Context) error {
	return f(ctx)
}
