package schedulerkit

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/executionlock"
)

type lifetimeLocker struct {
	ttl        time.Duration
	released   bool
	releaseErr error
}

func (l *lifetimeLocker) TryAcquire(_ context.Context, _ string, ttl time.Duration) (string, bool, error) {
	l.ttl = ttl
	return "owned", true, nil
}

func (l *lifetimeLocker) Release(ctx context.Context, _, _ string) error {
	l.released = true
	l.releaseErr = ctx.Err()

	return nil
}

func TestRunDigestCancelsBeforeLockTTLAndPreservesShorterDeadline(t *testing.T) {
	for _, budget := range []time.Duration{14 * time.Minute, 5 * time.Minute} {
		t.Run(budget.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()

				if budget == 5*time.Minute {
					var cancel context.CancelFunc

					ctx, cancel = context.WithTimeout(ctx, budget)

					defer cancel()
				}

				locker := &lifetimeLocker{}
				digest := NewDigestScheduler(locker, discardLogger())
				started := time.Now()
				err := digest.RunDigest(ctx, DigestOp[struct{}]{
					LockKey: "lifetime",
					Collect: func(ctx context.Context) (struct{}, bool, error) {
						deadline, ok := ctx.Deadline()
						if !ok || deadline.Sub(started) != budget {
							t.Fatalf("collect deadline = %v, want %v", deadline, started.Add(budget))
						}

						return struct{}{}, true, nil
					},
					Execute: func(ctx context.Context, _ struct{}) error {
						<-ctx.Done()

						return ctx.Err()
					},
				})

				if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != budget {
					t.Fatalf("elapsed=%v error=%v", time.Since(started), err)
				}

				if locker.ttl != executionlock.DefaultTTL || !locker.released || locker.releaseErr != nil {
					t.Fatalf("lock TTL/release = %+v", locker)
				}
			})
		})
	}
}

func TestRuntimeStopCancelsThenJoinsActiveTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := NewRuntime()
		entered := make(chan struct{})
		canceled := make(chan struct{})
		release := make(chan struct{})
		stopped := make(chan struct{})
		reason := make(chan StopReason, 1)
		first := true

		runtime.Start(t.Context(), &Config{
			Logger: discardLogger(),
			CalculateNextRun: func(now time.Time) time.Time {
				if first {
					first = false
					return now
				}

				return now.Add(time.Hour)
			},
			OnTick: func(ctx context.Context) {
				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release
			},
			OnStop: func(value StopReason) { reason <- value },
		})
		<-entered

		go func() {
			runtime.Stop()
			close(stopped)
		}()

		<-canceled
		synctest.Wait()

		select {
		case <-stopped:
			t.Fatal("Stop returned before the canceled tick joined")
		default:
		}

		close(release)
		<-stopped

		if got := <-reason; got != StopReasonManual {
			t.Fatalf("stop reason = %v", got)
		}
	})
}
