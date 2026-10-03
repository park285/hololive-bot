package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
)

type aggregatePhotoTask func(context.Context)

func (f aggregatePhotoTask) Start(ctx context.Context) { f(ctx) }

func TestRuntimeCloseContextResumesActualPlaneCleanupAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		var cleanups atomic.Int32

		admin := &adminruntime.AdminAPIRuntime{
			Managed: lifecycle.NewManaged(func() { cleanups.Add(1) }),
			PhotoSync: aggregatePhotoTask(func(ctx context.Context) {
				close(entered)
				<-ctx.Done()
				<-release
			}),
		}
		admin.Start(t.Context(), nil)
		<-entered

		runtime := &Runtime{closeSteps: []func(context.Context) error{admin.CloseContext}}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		firstErr := runtime.CloseContext(ctx)
		if !errors.Is(firstErr, context.DeadlineExceeded) || cleanups.Load() != 0 {
			t.Fatalf("active plane close = %v, cleanups = %d", firstErr, cleanups.Load())
		}

		finish()
		synctest.Wait()

		// 실제 plane은 종료 오류를 보존하면서도 완료된 photo 작업의 자원을 회수한다.
		for range 2 {
			if err := runtime.CloseContext(t.Context()); !errors.Is(err, firstErr) {
				t.Fatalf("resumed close lost its first error: %v", err)
			}
		}

		if got := cleanups.Load(); got != 1 {
			t.Fatalf("resumed plane cleanup count = %d, want 1", got)
		}
	})
}

func TestRuntimeCloseContextHonorsEachDeadlineAndRetainsActiveOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		var calls, tailCalls atomic.Int32

		runtime := &Runtime{closeSteps: []func(context.Context) error{
			func(context.Context) error {
				calls.Add(1)
				close(entered)
				<-release

				return nil
			},
			func(context.Context) error {
				tailCalls.Add(1)

				return nil
			},
		}}
		firstCtx, firstCancel := context.WithTimeout(t.Context(), 10*time.Second)

		defer firstCancel()

		firstDone := make(chan error, 1)

		go func() { firstDone <- runtime.CloseContext(firstCtx) }()

		<-entered

		for range 2 {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			start := time.Now()
			err := runtime.CloseContext(ctx)

			cancel()

			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != time.Second {
				t.Fatalf("waiting close = %v after %v, want caller deadline", err, time.Since(start))
			}
		}

		if err := <-firstDone; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first close = %v, want deadline", err)
		}

		if calls.Load() != 1 || tailCalls.Load() != 0 {
			t.Fatalf("active cleanup owner count = %d, tail count = %d", calls.Load(), tailCalls.Load())
		}

		finish()

		if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("completed close lost historical deadline: %v", err)
		}

		if calls.Load() != 1 || tailCalls.Load() != 1 {
			t.Fatalf("completed cleanup owner count = %d, tail count = %d, want 1/1", calls.Load(), tailCalls.Load())
		}
	})
}
