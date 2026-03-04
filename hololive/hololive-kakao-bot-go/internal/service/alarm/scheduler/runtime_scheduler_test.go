package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/checker"
)

type fakeRunner struct{}

func (f *fakeRunner) Check(_ context.Context) ([]*domain.AlarmNotification, error) {
	return []*domain.AlarmNotification{}, nil
}

type fakeSender struct{}

func (f *fakeSender) Send(_ context.Context, _ []*domain.AlarmNotification) (checker.SendResult, error) {
	return checker.SendResult{}, nil
}

func TestRuntimeSchedulerStart_CancellationPath(t *testing.T) {
	t.Parallel()

	runtimeScheduler := &RuntimeScheduler{
		youtubeChecker: &fakeRunner{},
		chzzkChecker:   &fakeRunner{},
		twitchChecker:  &fakeRunner{},
		notifier:       &fakeSender{},

		youtubeInterval: 5 * time.Second,
		chzzkInterval:   5 * time.Second,
		twitchInterval:  5 * time.Second,

		youtubeTimeout: 3 * time.Second,
		chzzkTimeout:   3 * time.Second,
		twitchTimeout:  3 * time.Second,

		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runtimeScheduler.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime scheduler did not stop after cancellation")
	}
}
