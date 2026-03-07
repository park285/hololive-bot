// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
