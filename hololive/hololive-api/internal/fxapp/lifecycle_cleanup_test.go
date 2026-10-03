package fxapp

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestLifecyclePreservesDrainAndCleanupErrors(t *testing.T) {
	drainErr := errors.New("listener drain failed after tasks joined")
	closeErr := errors.New("client close failed")
	runtime := &lifecycleTestRuntime{shutdown: func(context.Context) error { return drainErr }}
	owner := newResourceOwner()
	owner.Add(func(context.Context) error { return closeErr })

	coordinator := lifecycleTestCoordinator(runtime, owner)

	if err := coordinator.OnStart(t.Context()); err != nil {
		t.Fatal(err)
	}

	err := coordinator.OnStop(t.Context())
	if !errors.Is(err, drainErr) || !errors.Is(err, closeErr) {
		t.Fatalf("OnStop() = %v, want both drain and cleanup failures", err)
	}
}

func TestLifecycleUsesRemainingHardBudgetWithoutClosingActiveTaskResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		taskDone := make(chan struct{})
		resourceClosed := false
		runtime := &lifecycleTestRuntime{
			shutdown: func(ctx context.Context) error {
				<-ctx.Done()

				return ctx.Err()
			},
			closeRuntime: func(ctx context.Context) error {
				select {
				case <-taskDone:
					resourceClosed = true

					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
		owner := newResourceOwner()
		owner.Add(runtime.CloseContext)

		coordinator := lifecycleTestCoordinator(runtime, owner)

		coordinator.drainLimit = 10 * time.Second

		if err := coordinator.OnStart(t.Context()); err != nil {
			t.Fatal(err)
		}

		stopCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		start := time.Now()
		err := coordinator.OnStop(stopCtx)

		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 30*time.Second {
			t.Fatalf("OnStop() = %v after %v, want shared 30-second hard deadline", err, time.Since(start))
		}

		if resourceClosed {
			t.Fatal("resource was closed while its task remained active")
		}
	})
}
