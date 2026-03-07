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

package util

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func testCircuitLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForState(cb *CircuitBreaker, target CircuitState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cb.GetState() == target {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cb.GetState() == target
}

func TestCircuitStateString(t *testing.T) {
	t.Parallel()

	if CircuitStateClosed.String() != "CLOSED" {
		t.Fatalf("closed string = %q", CircuitStateClosed.String())
	}
	if CircuitStateOpen.String() != "OPEN" {
		t.Fatalf("open string = %q", CircuitStateOpen.String())
	}
	if CircuitStateHalfOpen.String() != "HALF_OPEN" {
		t.Fatalf("half-open string = %q", CircuitStateHalfOpen.String())
	}
}

func TestCircuitBreaker_OpensOnFailureThreshold(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(2, time.Second, 50*time.Millisecond, nil, testCircuitLogger())

	cb.RecordFailure(0)
	if got := cb.GetState(); got != CircuitStateClosed {
		t.Fatalf("state after 1 failure = %s, want %s", got, CircuitStateClosed)
	}

	cb.RecordFailure(0)
	if got := cb.GetState(); got != CircuitStateOpen {
		t.Fatalf("state after threshold failure = %s, want %s", got, CircuitStateOpen)
	}
	if cb.CanExecute() {
		t.Fatal("CanExecute() = true, want false in OPEN state")
	}

	status := cb.GetStatus()
	if status.NextRetryTime == nil {
		t.Fatal("NextRetryTime = nil, want non-nil in OPEN state")
	}
	if status.FailureCount != 2 {
		t.Fatalf("FailureCount = %d, want 2", status.FailureCount)
	}
}

func TestCircuitBreaker_HalfOpenAndCloseWithoutHealthCheck(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(1, 20*time.Millisecond, 10*time.Millisecond, nil, testCircuitLogger())
	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state = %s, want %s", cb.GetState(), CircuitStateOpen)
	}

	if ok := waitForState(cb, CircuitStateHalfOpen, 300*time.Millisecond); !ok {
		t.Fatalf("did not reach HALF_OPEN, current=%s", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != CircuitStateClosed {
		t.Fatalf("state after success = %s, want %s", cb.GetState(), CircuitStateClosed)
	}
	if status := cb.GetStatus(); status.FailureCount != 0 {
		t.Fatalf("FailureCount = %d, want 0", status.FailureCount)
	}
}

func TestCircuitBreaker_HealthCheckTransitionsToHalfOpen(t *testing.T) {
	t.Parallel()

	healthCalls := 0
	cb := NewCircuitBreaker(
		1,
		200*time.Millisecond,
		10*time.Millisecond,
		func() bool {
			healthCalls++
			return true
		},
		testCircuitLogger(),
	)

	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state = %s, want %s", cb.GetState(), CircuitStateOpen)
	}

	if ok := waitForState(cb, CircuitStateHalfOpen, time.Second); !ok {
		t.Fatalf("did not reach HALF_OPEN via health check, current=%s", cb.GetState())
	}
	if healthCalls == 0 {
		t.Fatal("health check calls = 0, want > 0")
	}

	cb.RecordSuccess()
	if cb.GetState() != CircuitStateClosed {
		t.Fatalf("state after success = %s, want %s", cb.GetState(), CircuitStateClosed)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(1, time.Second, 10*time.Millisecond, nil, testCircuitLogger())
	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state = %s, want %s", cb.GetState(), CircuitStateOpen)
	}

	cb.Reset()
	status := cb.GetStatus()
	if status.State != CircuitStateClosed {
		t.Fatalf("state after reset = %s, want %s", status.State, CircuitStateClosed)
	}
	if status.FailureCount != 0 {
		t.Fatalf("failure count after reset = %d, want 0", status.FailureCount)
	}
	if status.NextRetryTime != nil {
		t.Fatalf("next retry after reset = %v, want nil", status.NextRetryTime)
	}
}

func TestCircuitBreaker_CustomTimeout(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(1, time.Hour, 10*time.Millisecond, nil, testCircuitLogger())
	custom := 200 * time.Millisecond
	before := time.Now()

	cb.RecordFailure(custom)
	status := cb.GetStatus()
	if status.NextRetryTime == nil {
		t.Fatal("NextRetryTime = nil, want non-nil")
	}
	diff := status.NextRetryTime.Sub(before)
	if diff < 150*time.Millisecond || diff > 400*time.Millisecond {
		t.Fatalf("NextRetryTime delta = %s, want between 150ms and 400ms", diff)
	}
}
