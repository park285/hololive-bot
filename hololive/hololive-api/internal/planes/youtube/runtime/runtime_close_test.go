package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestCloseBeforeStartReleasesPoolOnceAndPreventsTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes, samples atomic.Int64

		runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

		runtime.closePool = func() { closes.Add(1) }
		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			samples.Add(1)

			return workercontract.QueueValues{}, nil
		})

		runtime.Close()
		runtime.Close()
		runtime.Start(t.Context(), make(chan error, 1))
		synctest.Wait()

		if closes.Load() != 1 || samples.Load() != 0 {
			t.Fatalf("close before start: closes=%d samples=%d", closes.Load(), samples.Load())
		}
	})
}

func TestCloseAdmissionPreventsFirstStartBeforePoolCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes, samples atomic.Int64

		runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.closePool = func() { closes.Add(1) }
		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			samples.Add(1)

			return workercontract.QueueValues{}, nil
		})

		t.Cleanup(runtime.Close)

		// Close의 첫 task snapshot 직후 실행을 양보한 스케줄을 제어한다.
		// 이 경계에서 첫 Start가 들어오면 nil snapshot을 믿고 pool을 먼저 닫을 수 있었다.
		tasksDone := runtime.beginClose()
		if tasksDone != nil {
			t.Fatal("unstarted runtime unexpectedly registered tasks")
		}

		var attempts sync.WaitGroup

		attempts.Go(func() { runtime.Start(t.Context(), make(chan error, 1)) })
		attempts.Wait()
		synctest.Wait()

		if samples.Load() != 0 || closes.Load() != 0 {
			t.Fatalf("start entered after close admission: samples=%d closes=%d", samples.Load(), closes.Load())
		}

		if err := runtime.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}

		closeClaimRuntimeConcurrently(t, runtime)
		runtime.Start(t.Context(), make(chan error, 1))
		synctest.Wait()

		if closes.Load() != 1 || samples.Load() != 0 {
			t.Fatalf("close before first start: closes=%d samples=%d", closes.Load(), samples.Load())
		}
	})
}

func TestShutdownBeforeStartKeepsStartAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes, samples atomic.Int64

		runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.closePool = func() { closes.Add(1) }
		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			samples.Add(1)

			return workercontract.QueueValues{}, nil
		})

		t.Cleanup(runtime.Close)

		if err := runtime.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}

		runtime.Start(t.Context(), make(chan error, 1))
		synctest.Wait()

		if samples.Load() != 1 || closes.Load() != 0 {
			t.Fatalf("shutdown before start blocked admission: samples=%d closes=%d", samples.Load(), closes.Load())
		}

		if err := runtime.CloseContext(t.Context()); err != nil {
			t.Fatal(err)
		}

		if closes.Load() != 1 {
			t.Fatalf("close after admitted start: closes=%d", closes.Load())
		}
	})
}

func TestShutdownReleaseKeepsCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		runtime := newTestRuntime(fakeClaimer{
			retry: func(ctx context.Context, _ sourceobservation.RetryInput) (contract.Status, error) {
				calls.Add(1)
				<-ctx.Done()

				return "", ctx.Err()
			},
		}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.Config.TransactionTimeout = time.Second
		runtime.remember(sourceobservation.ClaimWork{ObservationID: 1, LeaseToken: strings.Repeat("ab", 32)})
		runtime.Start(t.Context(), make(chan error, 1))
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
		defer cancel()

		started := time.Now()
		err := runtime.Shutdown(ctx)
		elapsed := time.Since(started)

		if !errors.Is(err, context.DeadlineExceeded) || elapsed > 25*time.Millisecond || calls.Load() != 1 {
			t.Fatalf("deadline-bound release: err=%v elapsed=%s calls=%d", err, elapsed, calls.Load())
		}

		if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Fatalf("release failure was retried or lost: err=%v calls=%d", err, calls.Load())
		}
	})
}

func TestShutdownReleaseIgnoresCancelOnlyParent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		runtime := newTestRuntime(fakeClaimer{
			retry: func(ctx context.Context, _ sourceobservation.RetryInput) (contract.Status, error) {
				calls.Add(1)

				if err := ctx.Err(); err != nil {
					t.Fatalf("cancel-only parent canceled fenced release: %v", err)
				}

				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("fenced release has no transaction deadline")
				}

				return contract.StatusPending, nil
			},
		}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.remember(sourceobservation.ClaimWork{ObservationID: 1, LeaseToken: strings.Repeat("ab", 32)})
		runtime.Start(t.Context(), make(chan error, 1))
		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := runtime.Shutdown(ctx); err != nil || calls.Load() != 1 {
			t.Fatalf("release after parent cancellation: err=%v calls=%d", err, calls.Load())
		}

		if err := runtime.CloseContext(t.Context()); err != nil || calls.Load() != 1 {
			t.Fatalf("close retried canceled-parent release: err=%v calls=%d", err, calls.Load())
		}
	})
}

func TestConcurrentCloseKeepsBudgetAndPoolWhileReleaseIsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })

		defer unblock()

		var calls, closes atomic.Int64

		runtime := newTestRuntime(fakeClaimer{
			retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
				calls.Add(1)
				close(entered)
				<-release

				return contract.StatusPending, nil
			},
		}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.closePool = func() { closes.Add(1) }
		runtime.remember(sourceobservation.ClaimWork{ObservationID: 1, LeaseToken: strings.Repeat("ab", 32)})
		runtime.Start(t.Context(), make(chan error, 1))

		done := shutdownAsync(t, runtime)
		awaitSignal(t, entered, "claim release did not start")

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		started := time.Now()
		err := runtime.CloseContext(ctx)
		elapsed := time.Since(started)

		if !errors.Is(err, context.DeadlineExceeded) || elapsed > 10*time.Millisecond || closes.Load() != 0 {
			t.Fatalf("close during release: err=%v elapsed=%s closes=%d", err, elapsed, closes.Load())
		}

		unblock()

		if err := <-done; err != nil {
			t.Fatalf("shutdown release: %v", err)
		}

		if err := runtime.CloseContext(t.Context()); err != nil || closes.Load() != 1 || calls.Load() != 1 {
			t.Fatalf("close after release: err=%v closes=%d calls=%d", err, closes.Load(), calls.Load())
		}
	})
}

func TestExhaustedShutdownDrainDoesNotAddReleaseBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })

		defer unblock()

		var calls, closes atomic.Int64

		runtime := newTestRuntime(fakeClaimer{
			retry: func(ctx context.Context, _ sourceobservation.RetryInput) (contract.Status, error) {
				calls.Add(1)
				<-ctx.Done()

				return "", ctx.Err()
			},
		}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.Config.ShutdownTimeout = time.Hour
		runtime.Config.TransactionTimeout = time.Second
		runtime.closePool = func() { closes.Add(1) }
		runtime.remember(sourceobservation.ClaimWork{ObservationID: 1, LeaseToken: strings.Repeat("ab", 32)})

		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			close(entered)
			<-release

			return workercontract.QueueValues{}, nil
		})
		runtime.Start(t.Context(), make(chan error, 1))
		awaitSignal(t, entered, "sampler did not start")

		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
		defer cancel()

		started := time.Now()
		err := runtime.Shutdown(ctx)
		elapsed := time.Since(started)

		if !errors.Is(err, context.DeadlineExceeded) || elapsed > 25*time.Millisecond || closes.Load() != 0 {
			t.Fatalf("exhausted drain: err=%v elapsed=%s closes=%d", err, elapsed, closes.Load())
		}

		unblock()

		if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) || closes.Load() != 1 || calls.Load() > 1 {
			t.Fatalf("close after exhausted drain: err=%v closes=%d calls=%d", err, closes.Load(), calls.Load())
		}
	})
}

func TestDisabledSamplerTimeoutKeepsPoolUntilLaterJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })

		defer unblock()

		var closes atomic.Int64

		runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

		runtime.Config.Enabled = false
		runtime.Config.ShutdownTimeout = 20 * time.Millisecond
		runtime.closePool = func() { closes.Add(1) }
		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			close(entered)
			<-release

			return workercontract.QueueValues{}, nil
		})
		runtime.Start(t.Context(), make(chan error, 1))
		awaitSignal(t, entered, "disabled runtime sampler did not start")

		if err := runtime.Shutdown(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown before sampler join = %v", err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		if err := runtime.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("close before sampler join = %v", err)
		}

		if err := runtime.Shutdown(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("repeated shutdown lost the unfinished sampler: %v", err)
		}

		if closes.Load() != 0 {
			t.Fatalf("pool closed while sampler was running: %d", closes.Load())
		}

		unblock()

		if err := runtime.CloseContext(t.Context()); err != nil {
			t.Fatalf("close after sampler join: %v", err)
		}

		runtime.Close()

		if closes.Load() != 1 {
			t.Fatalf("pool close count after sampler joined = %d", closes.Load())
		}
	})
}
