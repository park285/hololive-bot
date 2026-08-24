package workerruntime

import (
	"context"
	"errors"
	"fmt"
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

func TestNotificationEgressRunnerReturnsNilAfterRunnerExitOnContextCancel(t *testing.T) {
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	release := make(chan struct{})
	runnerExited := make(chan struct{})
	runner := notificationEgressRunner{runners: []NamedScheduler{
		{Name: "long-running", Scheduler: runtimeAlarmSchedulerFunc(func(runnerCtx context.Context) error {
			close(started)
			<-runnerCtx.Done()
			close(cancelObserved)
			<-release
			close(runnerExited)

			return nil
		})},
	}}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- runner.Start(ctx) }()

	<-started
	cancel()

	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not observe context cancel")
	}

	select {
	case err := <-done:
		t.Fatalf("Start returned before canceled runner exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after canceled runner exited")
	}

	select {
	case <-runnerExited:
	default:
		t.Fatal("Start returned before runner exit was observed")
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
	if err := f(ctx); err != nil {
		return fmt.Errorf("f: %w", err)
	}

	return nil
}

func TestAlarmWorkerRuntimeShutdownJoinsSchedulerExit(t *testing.T) {
	schedulerStarted := make(chan struct{})
	cancelObserved := make(chan struct{})
	release := make(chan struct{})
	schedulerExited := make(chan struct{})
	runtime := &AlarmWorkerRuntime{
		Scheduler: runtimeAlarmSchedulerFunc(func(ctx context.Context) error {
			close(schedulerStarted)
			<-ctx.Done()
			close(cancelObserved)
			<-release
			close(schedulerExited)

			return nil
		}),
	}

	runtime.Start(t.Context(), make(chan error, 1))

	select {
	case <-schedulerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not start")
	}

	shutdownDone := make(chan error, 1)

	go func() {
		shutdownDone <- runtime.Shutdown(t.Context())
	}()

	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not observe shutdown cancellation")
	}

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before scheduler exit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-schedulerExited:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit after release")
	}

	select {
	case err := <-shutdownDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after scheduler exit")
	}
}

func TestAlarmWorkerRuntimeShutdownHonorsContextDeadline(t *testing.T) {
	schedulerStarted := make(chan struct{})
	release := make(chan struct{})
	schedulerExited := make(chan struct{})
	runtime := &AlarmWorkerRuntime{
		Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
			close(schedulerStarted)
			<-release
			close(schedulerExited)

			return nil
		}),
	}

	runtime.Start(t.Context(), make(chan error, 1))

	select {
	case <-schedulerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	shutdownDone := make(chan error, 1)

	go func() {
		shutdownDone <- runtime.Shutdown(shutdownCtx)
	}()

	select {
	case <-shutdownCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown context did not reach its deadline")
	}

	select {
	case err := <-shutdownDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown blocked past its context deadline")
	}

	select {
	case <-schedulerExited:
		t.Fatal("scheduler exited despite ignoring cancellation")
	default:
	}

	close(release)

	select {
	case <-schedulerExited:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit after release")
	}
}

func TestAlarmWorkerRuntimeReportsSchedulerErrorOnErrCh(t *testing.T) {
	sentinel := errors.New("scheduler boom")
	runtime := &AlarmWorkerRuntime{
		Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
			return sentinel
		}),
	}
	errCh := make(chan error, 1)
	runtime.Start(t.Context(), errCh)

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "alarm runtime scheduler error")
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler error was not reported on errCh")
	}

	require.NoError(t, runtime.Shutdown(t.Context()))
}
