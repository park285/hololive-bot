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

package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-dispatcher-go/internal/dispatch"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type testQueueConsumer struct {
	drainBatchFunc      func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	releaseClaimKeyFunc func(ctx context.Context, claimKeys []string) error
	requeueFunc         func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

func (c *testQueueConsumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	if c.drainBatchFunc != nil {
		return c.drainBatchFunc(ctx, maxItems)
	}
	return nil, nil
}

func (c *testQueueConsumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c.releaseClaimKeyFunc != nil {
		return c.releaseClaimKeyFunc(ctx, claimKeys)
	}
	return nil
}

func (c *testQueueConsumer) Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if c.requeueFunc != nil {
		return c.requeueFunc(ctx, envelopes)
	}
	return nil
}

type testMessageSender struct {
	sendMessageFunc func(ctx context.Context, room, message string) error
}

func (s *testMessageSender) SendMessage(ctx context.Context, room, message string, _ ...iris.SendOption) error {
	if s.sendMessageFunc != nil {
		return s.sendMessageFunc(ctx, room, message)
	}
	return nil
}

var testLogger = sharedlogging.NewLogger

func newTestRuntimeForReadiness(connected bool) *Runtime {
	return &Runtime{
		logger: testLogger(),
		cacheSvc: &cachemocks.Client{
			IsConnectedFunc: func(context.Context) bool { return connected },
			CloseFunc:       func() error { return nil },
		},
		readyState: newReadinessState(),
	}
}

func TestBuildRuntime_NilConfig(t *testing.T) {
	t.Parallel()

	runtime, err := BuildRuntime(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("BuildRuntime() expected error for nil config, got nil")
	}
	if runtime != nil {
		t.Fatalf("BuildRuntime() runtime = %#v, want nil", runtime)
	}
}

func TestRuntimeNilReceiver_NoPanic(t *testing.T) {
	t.Parallel()

	var runtime *Runtime
	runtime.Close()
	runtime.Run()
}

func TestRuntimeRoutes_HealthAndReady(t *testing.T) {
	t.Parallel()

	t.Run("health endpoint returns ok", func(t *testing.T) {
		rt := newTestRuntimeForReadiness(true)
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		rt.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var payload map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode health response: %v", err)
		}
		if got := payload["status"]; got != "ok" {
			t.Fatalf("status field = %v, want ok", got)
		}
	})

	t.Run("ready endpoint reflects dispatcher and cache state", func(t *testing.T) {
		t.Run("not ready when dispatch loop stopped", func(t *testing.T) {
			rt := newTestRuntimeForReadiness(true)
			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()

			rt.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
			}
		})

		t.Run("ready when dispatch loop running and valkey connected", func(t *testing.T) {
			rt := newTestRuntimeForReadiness(true)
			rt.readyState.dispatchLoopRunning.Store(true)
			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()

			rt.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			var payload map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
				t.Fatalf("decode ready response: %v", err)
			}
			if got := payload["status"]; got != "ready" {
				t.Fatalf("status field = %v, want ready", got)
			}
		})

		t.Run("last_error is hidden when present internally", func(t *testing.T) {
			rt := newTestRuntimeForReadiness(false)
			rt.readyState.dispatchLoopRunning.Store(true)
			rt.readyState.setLastError("dispatch failed")

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()
			rt.routes().ServeHTTP(rec, req)

			var payload map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
				t.Fatalf("decode ready response: %v", err)
			}
			if _, exists := payload["last_error"]; exists {
				t.Fatal("last_error should be hidden from readiness payload")
			}
		})
	})
}

func TestRunDispatchLoop_ErrorThenRecoveryClearsLastError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	consumer := &testQueueConsumer{
		drainBatchFunc: func(_ context.Context, _ int) ([]domain.AlarmQueueEnvelope, error) {
			switch calls.Add(1) {
			case 1:
				return nil, errors.New("boom")
			case 2:
				cancel()
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	dispatcher, err := dispatch.NewDispatcher(
		consumer,
		&testMessageSender{},
		dispatch.NewSimpleRenderer(),
		1,
		1,
		testLogger(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	rt := &Runtime{
		cfg: &Config{
			Dispatch: DispatchConfig{ReconnectBackoff: 1 * time.Millisecond},
		},
		logger:     testLogger(),
		dispatcher: dispatcher,
		readyState: newReadinessState(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rt.runDispatchLoop(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runDispatchLoop() did not stop in time")
	}

	if got := calls.Load(); got < 2 {
		t.Fatalf("DrainBatch calls = %d, want >= 2", got)
	}
	if lastErr := rt.readyState.getLastError(); lastErr != "" {
		t.Fatalf("last error = %q, want empty after recovery", lastErr)
	}
	if rt.readyState.dispatchLoopRunning.Load() {
		t.Fatal("dispatch loop running flag should be false after stop")
	}
}

func TestRunDispatchLoop_CancelDuringBackoffStopsQuickly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstCall := make(chan struct{})
	consumer := &testQueueConsumer{
		drainBatchFunc: func(_ context.Context, _ int) ([]domain.AlarmQueueEnvelope, error) {
			select {
			case <-firstCall:
			default:
				close(firstCall)
			}
			return nil, errors.New("temporary failure")
		},
	}

	dispatcher, err := dispatch.NewDispatcher(
		consumer,
		&testMessageSender{},
		dispatch.NewSimpleRenderer(),
		1,
		1,
		testLogger(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	rt := &Runtime{
		cfg: &Config{
			Dispatch: DispatchConfig{ReconnectBackoff: 5 * time.Second},
		},
		logger:     testLogger(),
		dispatcher: dispatcher,
		readyState: newReadinessState(),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rt.runDispatchLoop(ctx)
	}()

	select {
	case <-firstCall:
	case <-time.After(2 * time.Second):
		t.Fatal("first dispatch call was not observed")
	}

	start := time.Now()
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runDispatchLoop() did not stop quickly after cancel")
	}

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("stop elapsed = %s, want <= 500ms", elapsed)
	}
}
