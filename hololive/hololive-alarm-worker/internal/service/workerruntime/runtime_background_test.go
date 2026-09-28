package workerruntime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestBackgroundFailureCancelsAndJoinsSiblings(t *testing.T) {
	for _, panicChild := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicChild], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sentinel := errors.New("child failed")
				started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
				runtime := &AlarmWorkerRuntime{
					Logger: slog.New(slog.DiscardHandler),
					Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error {
						<-started

						if panicChild {
							panic("child panic")
						}

						return sentinel
					}),
					NotificationEgress: runtimeAlarmSchedulerFunc(func(ctx context.Context) error {
						close(started)
						<-ctx.Done()
						close(canceled)
						<-release

						return nil
					}),
				}
				errCh := make(chan error, 1)
				runtime.Start(t.Context(), errCh)
				<-canceled
				synctest.Wait()

				select {
				case err := <-errCh:
					t.Fatalf("failure returned before sibling exit: %v", err)
				default:
				}

				close(release)

				err := <-errCh

				if panicChild {
					require.ErrorContains(t, err, "child panic")
				} else {
					require.ErrorIs(t, err, sentinel)
				}

				require.NoError(t, runtime.Shutdown(t.Context()))
			})
		})
	}
}
