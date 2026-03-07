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

package holodex

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryScheduler_ExecutesAfterDelay(t *testing.T) {
	scheduler := newRetryScheduler(20*time.Millisecond, 50*time.Millisecond, 10, slog.Default())
	defer scheduler.stop()

	done := make(chan struct{})
	scheduler.schedule("k1", func(ctx context.Context) {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("retry callback did not execute in time")
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for scheduler.pendingCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := scheduler.pendingCount(); got != 0 {
		t.Fatalf("pending count mismatch: got %d want 0", got)
	}
}

func TestRetryScheduler_Dedup(t *testing.T) {
	scheduler := newRetryScheduler(50*time.Millisecond, 50*time.Millisecond, 10, slog.Default())
	defer scheduler.stop()

	scheduler.schedule("same-key", func(ctx context.Context) {})
	scheduler.schedule("same-key", func(ctx context.Context) {})

	if got := scheduler.pendingCount(); got != 1 {
		t.Fatalf("pending count mismatch: got %d want 1", got)
	}
}

func TestRetryScheduler_MaxSizeOverflow(t *testing.T) {
	scheduler := newRetryScheduler(50*time.Millisecond, 50*time.Millisecond, 2, slog.Default())
	defer scheduler.stop()

	scheduler.schedule("k1", func(ctx context.Context) {})
	scheduler.schedule("k2", func(ctx context.Context) {})
	before := scheduler.pendingCount()
	scheduler.schedule("k3", func(ctx context.Context) {})

	if before != 2 {
		t.Fatalf("unexpected initial pending count: got %d want 2", before)
	}
	if got := scheduler.pendingCount(); got != before {
		t.Fatalf("pending count should remain unchanged: got %d want %d", got, before)
	}
}

func TestRetryScheduler_Stop_CancelsPending(t *testing.T) {
	scheduler := newRetryScheduler(50*time.Millisecond, 50*time.Millisecond, 10, slog.Default())

	var called atomic.Int32
	scheduler.schedule("k1", func(ctx context.Context) {
		called.Add(1)
	})

	scheduler.stop()
	time.Sleep(80 * time.Millisecond)

	if got := called.Load(); got != 0 {
		t.Fatalf("callback should not be called after stop: got %d", got)
	}
	if got := scheduler.pendingCount(); got != 0 {
		t.Fatalf("pending count mismatch after stop: got %d want 0", got)
	}
}

func TestRetryScheduler_Stop_WaitsExecuting(t *testing.T) {
	scheduler := newRetryScheduler(10*time.Millisecond, 50*time.Millisecond, 10, slog.Default())

	started := make(chan struct{})
	release := make(chan struct{})
	stopped := make(chan struct{})

	scheduler.schedule("k1", func(ctx context.Context) {
		close(started)
		<-release
	})

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("callback did not start in time")
	}

	go func() {
		scheduler.stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("stop returned before callback finished")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)

	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stop did not wait for callback completion")
	}
}

func TestRetryScheduler_IsRetryContext(t *testing.T) {
	if isRetryContext(context.Background()) {
		t.Fatal("background context should not be retry context")
	}

	ctx := context.WithValue(context.Background(), retryContextKey{}, true)
	if !isRetryContext(ctx) {
		t.Fatal("retry-marked context should be recognized")
	}
}

func TestRetryScheduler_Execute_HasTimeout(t *testing.T) {
	scheduler := newRetryScheduler(10*time.Millisecond, 50*time.Millisecond, 10, slog.Default())
	defer scheduler.stop()

	done := make(chan struct{})
	var deadlineSet atomic.Bool
	var retryMarked atomic.Bool

	scheduler.execute("k1", func(ctx context.Context) {
		_, ok := ctx.Deadline()
		deadlineSet.Store(ok)
		retryMarked.Store(isRetryContext(ctx))
		close(done)
	})

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("execute callback did not run in time")
	}

	if !deadlineSet.Load() {
		t.Fatal("retry context should have timeout deadline")
	}
	if !retryMarked.Load() {
		t.Fatal("retry context marker should be set")
	}
}

func TestRetryScheduler_SkipAfterStopped(t *testing.T) {
	scheduler := newRetryScheduler(10*time.Millisecond, 50*time.Millisecond, 10, slog.Default())

	var called atomic.Int32
	scheduler.stop()
	scheduler.schedule("k1", func(ctx context.Context) {
		called.Add(1)
	})

	time.Sleep(30 * time.Millisecond)

	if got := scheduler.pendingCount(); got != 0 {
		t.Fatalf("pending count should be 0 after stopped schedule: got %d", got)
	}
	if got := called.Load(); got != 0 {
		t.Fatalf("callback should not execute after stop: got %d", got)
	}
}
