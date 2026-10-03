package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
)

type resourcePhotoTask func(context.Context)

func (f resourcePhotoTask) Start(ctx context.Context) { f(ctx) }

type resourceAdminRuntime struct {
	admin         *adminruntime.AdminAPIRuntime
	closeReturned chan struct{}
	closeOnce     sync.Once
}

func (r *resourceAdminRuntime) Start(ctx context.Context, errs chan<- error) {
	r.admin.Start(ctx, errs)
}

func (r *resourceAdminRuntime) Shutdown(ctx context.Context) error { return r.admin.Shutdown(ctx) }

func (r *resourceAdminRuntime) CloseContext(ctx context.Context) error {
	err := r.admin.CloseContext(ctx)
	r.closeOnce.Do(func() { close(r.closeReturned) })

	if err != nil {
		return fmt.Errorf("close admin plane: %w", err)
	}

	return nil
}

type resourceTelemetry struct {
	calls atomic.Int32
	err   error
}

func (r *resourceTelemetry) Shutdown(context.Context) error {
	r.calls.Add(1)

	return r.err
}

func TestApplicationSafetyCloseResumesActualPlaneAfterStopDeadline(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })

	defer finish()

	var cleanups atomic.Int32

	admin := &adminruntime.AdminAPIRuntime{
		Managed: lifecycle.NewManaged(func() { cleanups.Add(1) }),
		PhotoSync: resourcePhotoTask(func(ctx context.Context) {
			close(entered)
			<-ctx.Done()
			<-release
			close(exited)
		}),
	}
	runtime := &resourceAdminRuntime{admin: admin, closeReturned: make(chan struct{})}
	telemetryErr := errors.New("telemetry close failed")
	provider := &resourceTelemetry{err: telemetryErr}
	params := successfulApplicationParams(nil)

	params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
		return provider, nil
	}
	params.dependencies.buildRuntime = func(context.Context, *apiconfig.RuntimeConfig, *slog.Logger) (runtimeResource, error) {
		return runtime, nil
	}

	application, err := newApplication(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}

	application.coordinator.drainLimit = 10 * time.Millisecond

	if err := application.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	<-entered

	stopCtx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)

	defer cancel()

	if err := application.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want deadline", err)
	}

	<-runtime.closeReturned

	if got := cleanups.Load(); got != 0 {
		t.Fatalf("active plane cleanup count = %d, want 0", got)
	}

	finish()
	<-exited
	application.SafetyClose(t.Context())

	if err := application.resources.Close(t.Context()); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, telemetryErr) {
		t.Fatalf("resumed close lost first deadline or tail error: %v", err)
	}

	if cleanups.Load() != 1 || provider.calls.Load() != 1 {
		t.Fatalf("resumed plane cleanup = %d, telemetry shutdown = %d, want 1/1", cleanups.Load(), provider.calls.Load())
	}
}

func TestResourceOwnerRetainsActiveCleanupAndEachCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		var calls, tailCalls atomic.Int32

		owner := newResourceOwner()
		owner.Add(func(context.Context) error {
			tailCalls.Add(1)

			return nil
		})
		owner.AddResumable(func(context.Context) error {
			calls.Add(1)
			close(entered)
			<-release

			return nil
		})

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		if err := owner.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("active cleanup close = %v, want deadline", err)
		}

		<-entered

		waitingCtx, waitingCancel := context.WithTimeout(t.Context(), time.Second)

		defer waitingCancel()

		start := time.Now()
		if err := owner.Close(waitingCtx); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != time.Second {
			t.Fatalf("waiting close = %v after %v, want caller deadline", err, time.Since(start))
		}

		if calls.Load() != 1 || tailCalls.Load() != 0 {
			t.Fatalf("active cleanup count = %d, tail count = %d, want 1/0", calls.Load(), tailCalls.Load())
		}

		finish()

		if err := owner.Close(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("completed close lost historical deadline: %v", err)
		}

		if calls.Load() != 1 || tailCalls.Load() != 1 {
			t.Fatalf("completed cleanup count = %d, tail count = %d, want 1/1", calls.Load(), tailCalls.Load())
		}
	})
}
