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

package botruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testWebhookCloser struct {
	calls int
	err   error
}

func TestShutdownWebhookAndDurabilityAlwaysStopsWorkersAndJoinsErrors(t *testing.T) {
	closeErr := errors.New("webhook close failed")
	webhookCloser := &testWebhookCloser{err: closeErr}
	workerCtx, cancelWorker := context.WithCancel(t.Context())
	durable := &durableRuntime{cancel: cancelWorker}
	durable.wg.Add(1)

	workerDone := make(chan struct{})

	go func() {
		defer durable.wg.Done()

		<-workerCtx.Done()
		close(workerDone)
	}()

	runtime := &BotRuntime{webhookHandlerCloser: webhookCloser, durable: durable}
	shutdownCtx, cancelShutdown := context.WithCancel(t.Context())
	cancelShutdown()

	err := runtime.shutdownWebhookAndDurability(shutdownCtx)
	if !errors.Is(err, closeErr) {
		t.Fatalf("joined shutdown error = %v", err)
	}

	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("durable worker was not stopped")
	}
}

func TestDurableStopReturnsAtDeadlineWhenWorkerDoesNotExit(t *testing.T) {
	canceled := make(chan struct{})
	r := &durableRuntime{cancel: func() { close(canceled) }}
	r.wg.Add(1)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)

	defer cancel()

	started := time.Now()
	err := r.Stop(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v", err)
	}

	if time.Since(started) > time.Second {
		t.Fatal("Stop() did not honor its deadline")
	}

	select {
	case <-canceled:
	default:
		t.Fatal("Stop() did not cancel before waiting")
	}

	r.wg.Done()
}

func (c *testWebhookCloser) CloseContext(context.Context) error {
	c.calls++
	return c.err
}

func TestBotRuntimeShutdown_ClosesWebhookHandler(t *testing.T) {
	webhookCloser := &testWebhookCloser{}
	runtime := &BotRuntime{webhookHandlerCloser: webhookCloser}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook CloseContext calls = %d, want %d", webhookCloser.calls, 1)
	}
}
