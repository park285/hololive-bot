package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type fakeRunner struct{}

func (f *fakeRunner) Check(context.Context, targetpolicy.TargetMinutePolicy) ([]*domain.AlarmNotification, error) {
	return []*domain.AlarmNotification{}, nil
}

type fakeSender struct{}

func (f *fakeSender) Send(context.Context, []*domain.AlarmNotification, targetpolicy.TargetMinutePolicy) (delivery.SendResult, error) {
	return delivery.SendResult{}, nil
}

func TestRuntimeSchedulerStart_CancellationPath(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runtimeScheduler := &RuntimeScheduler{
			youtubeChecker:      &fakeRunner{},
			notifier:            &fakeSender{},
			targetMinutesSource: &targetMinutesSourceStub{targets: []int{5, 3, 1}},

			youtubeInterval: 5 * time.Second,

			youtubeTimeout: 3 * time.Second,

			logger: slog.New(slog.DiscardHandler),
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() {
			done <- runtimeScheduler.Start(ctx)
		}()

		synctest.Wait()
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("runtime scheduler did not stop after cancellation")
		}
	})
}

func TestNewRuntimeSchedulerRejectsMissingDependencies(t *testing.T) {
	scheduler, err := NewRuntimeScheduler(Dependencies{})
	require.ErrorContains(t, err, "cache service is nil")
	require.Nil(t, scheduler)
}
