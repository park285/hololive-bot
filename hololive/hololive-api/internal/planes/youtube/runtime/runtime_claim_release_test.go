package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestShutdownDefersClaimReleaseUntilBatchRegistrationCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newClaimRegistrationFixture(t, nil)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)

		defer cancel()

		if err := fixture.runtime.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown before registration completed: %v", err)
		}

		if fixture.claims.Load() != 1 || fixture.retries.Load() != 0 || fixture.closes.Load() != 0 || fixture.runtime.releaseDone != nil {
			t.Fatalf("unfinished producer sealed release: claims=%d retries=%d closes=%d release=%v", fixture.claims.Load(), fixture.retries.Load(), fixture.closes.Load(), fixture.runtime.releaseDone)
		}

		fixture.unblock()
		closeClaimRuntimeConcurrently(t, fixture.runtime)

		if _, remembered := fixture.runtime.inFlight.Load(fixture.work.Key()); remembered || fixture.retries.Load() != 1 || fixture.closes.Load() != 1 {
			t.Fatalf("late claim cleanup: remembered=%t retries=%d closes=%d", remembered, fixture.retries.Load(), fixture.closes.Load())
		}
	})
}

func TestCanceledShutdownSettlesLateClaimWithinExistingBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newClaimRegistrationFixture(t, nil)
		unblockClaimRegistrationAfter(t, fixture, 10*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		started := time.Now()
		err := fixture.runtime.Shutdown(ctx)

		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled shutdown: %v", err)
		}

		if elapsed := time.Since(started); elapsed != 10*time.Millisecond || fixture.retries.Load() != 1 {
			t.Fatalf("late claim did not settle after cancel-only parent: elapsed=%s retries=%d", elapsed, fixture.retries.Load())
		}

		if err := fixture.runtime.CloseContext(t.Context()); err != nil {
			t.Fatal(err)
		}

		if _, remembered := fixture.runtime.inFlight.Load(fixture.work.Key()); remembered || fixture.retries.Load() != 1 || fixture.closes.Load() != 1 {
			t.Fatalf("canceled shutdown cleanup: remembered=%t retries=%d closes=%d", remembered, fixture.retries.Load(), fixture.closes.Load())
		}
	})
}

func TestCanceledShutdownSharesRegistrationAndReleaseBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newClaimRegistrationFixture(t, func(ctx context.Context) error {
			<-ctx.Done()

			return ctx.Err()
		})
		unblockClaimRegistrationAfter(t, fixture, 10*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		started := time.Now()
		err := fixture.runtime.Shutdown(ctx)
		elapsed := time.Since(started)

		if !errors.Is(err, context.DeadlineExceeded) || elapsed != fixture.runtime.Config.TransactionTimeout || fixture.retries.Load() != 1 {
			t.Fatalf("settlement budget extended: err=%v elapsed=%s retries=%d", err, elapsed, fixture.retries.Load())
		}

		if err := fixture.runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) || fixture.retries.Load() != 1 || fixture.closes.Load() != 1 {
			t.Fatalf("failed settlement retried or lost: err=%v retries=%d closes=%d", err, fixture.retries.Load(), fixture.closes.Load())
		}
	})
}

func TestCanceledShutdownDefersReleaseWhenRegistrationBudgetExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newClaimRegistrationFixture(t, nil)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		started := time.Now()
		err := fixture.runtime.Shutdown(ctx)
		elapsed := time.Since(started)

		if !errors.Is(err, context.DeadlineExceeded) || elapsed != fixture.runtime.Config.TransactionTimeout || fixture.retries.Load() != 0 || fixture.runtime.releaseDone != nil {
			t.Fatalf("unfinished registration sealed release: err=%v elapsed=%s retries=%d release=%v", err, elapsed, fixture.retries.Load(), fixture.runtime.releaseDone)
		}

		fixture.unblock()

		if err := fixture.runtime.CloseContext(t.Context()); err != nil || fixture.retries.Load() != 1 || fixture.closes.Load() != 1 {
			t.Fatalf("later close missed registered claim: err=%v retries=%d closes=%d", err, fixture.retries.Load(), fixture.closes.Load())
		}
	})
}

func TestDelayedWorkerDoesNotRegisterReleasedClaim(t *testing.T) {
	work := sourceobservation.ClaimWork{ObservationID: 43, LeaseToken: strings.Repeat("cd", 32)}

	var retries atomic.Int64

	runtime := newTestRuntime(fakeClaimer{
		retry: func(_ context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			retries.Add(1)

			if input.ObservationID != work.ObservationID || input.LeaseToken != work.LeaseToken {
				t.Errorf("released another claim: %#v", input)
			}

			return contract.StatusPending, nil
		},
	}, fakeConsumer{
		consume: func(ctx context.Context, _ sourceobservation.Claim) error {
			return ctx.Err()
		},
	})
	// 이미 batch에 등록된 claim을 해제한 뒤, 늦은 worker의 실제 처리 경로를 실행합니다.
	runtime.Config.Enabled = false
	runtime.remember(work)
	runtime.Start(t.Context(), make(chan error, 1))

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := runtime.processClaim(ctx, work); err != nil {
		t.Fatalf("delayed canceled worker: %v", err)
	}

	if err := runtime.CloseContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, remembered := runtime.inFlight.Load(work.Key()); remembered || retries.Load() != 1 {
		t.Fatalf("released token registered again: remembered=%t retries=%d", remembered, retries.Load())
	}
}

type claimRegistrationFixture struct {
	runtime                 *Runtime
	work                    sourceobservation.ClaimWork
	claims, retries, closes atomic.Int64
	unblock                 func()
}

func newClaimRegistrationFixture(t *testing.T, settle func(context.Context) error) *claimRegistrationFixture {
	t.Helper()

	f := &claimRegistrationFixture{work: sourceobservation.ClaimWork{
		ObservationID:   42,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_LATE",
	}}

	f.runtime = newTestRuntime(fakeClaimer{
		claim: func(ctx context.Context, _ sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
			f.claims.Add(1)

			if err := ctx.Err(); err != nil {
				return sourceobservation.ClaimedBatch{}, err
			}

			return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{f.work}}, nil
		},
		retry: func(ctx context.Context, input sourceobservation.RetryInput) (contract.Status, error) {
			f.retries.Add(1)

			if input.ObservationID != f.work.ObservationID || input.LeaseToken != f.work.LeaseToken {
				t.Errorf("released another claim: %#v", input)
			}

			if settle != nil {
				if err := settle(ctx); err != nil {
					return "", err
				}
			}

			return contract.StatusPending, nil
		},
	}, fakeConsumer{})
	f.runtime.Config.Retention.Enabled = false
	f.runtime.Config.Replay.Enabled = false
	f.runtime.Config.LiveEndFinalizer.Enabled = false
	f.runtime.Config.TransactionTimeout = 25 * time.Millisecond
	f.runtime.workCh = make(chan sourceobservation.ClaimWork)
	f.runtime.closePool = func() { f.closes.Add(1) }

	entered, unblock := pauseClaimRegistration(t, f.runtime)

	f.unblock = unblock
	f.runtime.Start(t.Context(), make(chan error, 1))
	awaitSignal(t, entered, "claimed batch did not reach registration")

	return f
}

func pauseClaimRegistration(t *testing.T, runtime *Runtime) (<-chan struct{}, func()) {
	t.Helper()

	entered := make(chan struct{})
	resume := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(resume) })

	t.Cleanup(func() {
		runtime.stopTasks()
		unblock()
		synctest.Wait()
	})

	gate := sync.OnceFunc(func() {
		close(entered)
		<-resume
	})

	// DB 선점 성공 후 등록 전의 지연을 실제 claimer 경계에서 재현한다.
	runtime.claimer = &pausedRegistrationClaimer{observationClaimer: runtime.claimer, pause: gate}

	return entered, unblock
}

type pausedRegistrationClaimer struct {
	observationClaimer

	pause func()
}

func (c *pausedRegistrationClaimer) ClaimBatch(ctx context.Context, options sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
	batch, err := c.observationClaimer.ClaimBatch(ctx, options)
	if err != nil {
		return sourceobservation.ClaimedBatch{}, fmt.Errorf("claim before registration pause: %w", err)
	}

	c.pause()

	return batch, nil
}

func unblockClaimRegistrationAfter(t *testing.T, f *claimRegistrationFixture, delay time.Duration) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	t.Cleanup(func() {
		cancel()
		<-done
	})

	go func() {
		defer close(done)

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
			f.unblock()
		case <-ctx.Done():
		}
	}()
}

func closeClaimRuntimeConcurrently(t *testing.T, runtime *Runtime) {
	t.Helper()

	var wg sync.WaitGroup

	for range 3 {
		wg.Go(func() {
			if err := runtime.CloseContext(t.Context()); err != nil {
				t.Errorf("close after registration completed: %v", err)
			}
		})
	}

	wg.Go(func() {
		if err := runtime.Shutdown(t.Context()); err != nil {
			t.Errorf("shutdown after registration completed: %v", err)
		}
	})
	wg.Wait()
}
