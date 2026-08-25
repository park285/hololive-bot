package fxapp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestLifecycleCancelsRuntimeBeforeShutdownAndClosesResources(t *testing.T) {
	var (
		calls       []string
		runtimeDone <-chan struct{}
	)

	runtime := &lifecycleTestRuntime{start: func(ctx context.Context, _ chan<- error) {
		runtimeDone = ctx.Done()
	}}

	runtime.shutdown = func(context.Context) error {
		select {
		case <-runtimeDone:
			calls = append(calls, "shutdown")
		default:
			t.Fatal("runtime context was not canceled before shutdown")
		}

		return nil
	}

	owner := newResourceOwner()
	owner.Add(func(context.Context) { calls = append(calls, "close") })

	coordinator := lifecycleTestCoordinator(runtime, owner)

	if err := coordinator.OnStart(t.Context()); err != nil {
		t.Fatalf("OnStart() error = %v", err)
	}

	if err := coordinator.OnStop(t.Context()); err != nil {
		t.Fatalf("OnStop() error = %v", err)
	}

	if want := []string{"shutdown", "close"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("lifecycle calls = %v, want %v", calls, want)
	}
}

func TestLifecycleJoinsFatalAndShutdownErrors(t *testing.T) {
	fatalErr := errors.New("listener failed")
	shutdownErr := errors.New("admin drain failed")
	runtime := &lifecycleTestRuntime{shutdown: func(context.Context) error { return shutdownErr }}
	shutdowner := newSupervisorTestShutdowner()
	supervisor := newSupervisorWithShutdowner(shutdowner, supervisorTestLogger())
	coordinator := &lifecycleCoordinator{
		runtime:    runtime,
		resources:  newResourceOwner(),
		supervisor: supervisor,
		logger:     supervisorTestLogger(),
		drainLimit: time.Second,
	}

	if err := coordinator.OnStart(t.Context()); err != nil {
		t.Fatalf("OnStart() error = %v", err)
	}

	runtime.errCh <- fatalErr

	receiveShutdownCall(t, shutdowner.calls)

	err := coordinator.OnStop(t.Context())

	if !errors.Is(err, fatalErr) || !errors.Is(err, shutdownErr) {
		t.Fatalf("OnStop() error = %v, want fatal and shutdown errors", err)
	}
}

func TestLifecycleAppliesBoundedPlaneDrainAndStillCloses(t *testing.T) {
	closed := false
	runtime := &lifecycleTestRuntime{shutdown: func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}}
	owner := newResourceOwner()
	owner.Add(func(context.Context) { closed = true })

	coordinator := lifecycleTestCoordinator(runtime, owner)

	coordinator.drainLimit = 10 * time.Millisecond

	if err := coordinator.OnStart(t.Context()); err != nil {
		t.Fatalf("OnStart() error = %v", err)
	}

	err := coordinator.OnStop(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OnStop() error = %v, want deadline exceeded", err)
	}

	if !closed {
		t.Fatal("resource owner was not closed after bounded drain failure")
	}
}

type lifecycleTestRuntime struct {
	start    func(context.Context, chan<- error)
	errCh    chan<- error
	shutdown func(context.Context) error
}

func (r *lifecycleTestRuntime) Start(ctx context.Context, errCh chan<- error) {
	r.errCh = errCh

	if r.start != nil {
		r.start(ctx, errCh)
	}
}

func (r *lifecycleTestRuntime) Shutdown(ctx context.Context) error {
	if r.shutdown == nil {
		return nil
	}

	if err := r.shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown hook: %w", err)
	}

	return nil
}

func (r *lifecycleTestRuntime) Close() {}

func lifecycleTestCoordinator(runtime runtimeResource, owner *resourceOwner) *lifecycleCoordinator {
	return &lifecycleCoordinator{
		runtime:    runtime,
		resources:  owner,
		supervisor: newSupervisorWithShutdowner(newSupervisorTestShutdowner(), supervisorTestLogger()),
		logger:     supervisorTestLogger(),
		drainLimit: time.Second,
	}
}
