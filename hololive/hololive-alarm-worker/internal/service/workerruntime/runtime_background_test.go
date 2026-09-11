package workerruntime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

type blockingSubscriberClient struct {
	valkey.Client

	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (*blockingSubscriberClient) B() valkey.Builder { return valkey.Builder{} }
func (c *blockingSubscriberClient) Receive(ctx context.Context, _ valkey.Completed, _ func(valkey.PubSubMessage)) error {
	close(c.started)
	<-ctx.Done()
	close(c.canceled)
	<-c.release

	return ctx.Err()
}

func TestRuntimeShutdownJoinsConcreteSubscriberRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &blockingSubscriberClient{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
		runtime := &AlarmWorkerRuntime{ConfigSubscriber: configsub.New(client, nil, slog.New(slog.DiscardHandler))}
		runtime.Start(t.Context(), make(chan error, 1))
		<-client.started

		done := make(chan error, 1)

		go func() { done <- runtime.Shutdown(t.Context()) }()

		synctest.Wait()

		select {
		case <-client.canceled:
		default:
			t.Fatal("subscriber did not observe shutdown")
		}

		select {
		case err := <-done:
			t.Fatalf("shutdown preceded subscriber exit: %v", err)
		default:
		}

		close(client.release)
		require.NoError(t, <-done)
	})
}

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
