package util

import (
	"testing"
	"time"
)

func TestCircuitBreaker_RecordSuccessResetsFailureCountInClosed(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(3, time.Second, 10*time.Millisecond, nil, testCircuitLogger())

	cb.RecordFailure(0)
	if s := cb.GetStatus(); s.FailureCount != 1 {
		t.Fatalf("FailureCount = %d, want 1", s.FailureCount)
	}

	cb.RecordSuccess()
	if s := cb.GetStatus(); s.FailureCount != 0 {
		t.Fatalf("FailureCount after RecordSuccess in CLOSED = %d, want 0", s.FailureCount)
	}
	if s := cb.GetStatus(); s.State != CircuitStateClosed {
		t.Fatalf("State = %s, want %s", s.State, CircuitStateClosed)
	}
}

func TestCircuitBreaker_FailureInHalfOpenReopensCircuit(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(1, 20*time.Millisecond, 10*time.Millisecond, nil, testCircuitLogger())
	cb.RecordFailure(0)

	if ok := waitForState(cb, CircuitStateHalfOpen, 300*time.Millisecond); !ok {
		t.Fatalf("did not reach HALF_OPEN, current=%s", cb.GetState())
	}

	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state after failure in HALF_OPEN = %s, want %s", cb.GetState(), CircuitStateOpen)
	}
}

func TestCircuitBreaker_HealthCheckFailedDelaysNextCheck(t *testing.T) {
	t.Parallel()

	healthCalls := 0
	cb := NewCircuitBreaker(
		1,
		200*time.Millisecond,
		10*time.Millisecond,
		func() bool {
			healthCalls++
			return false
		},
		testCircuitLogger(),
	)

	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state = %s, want %s", cb.GetState(), CircuitStateOpen)
	}

	time.Sleep(50 * time.Millisecond)

	cb.GetState()
	time.Sleep(30 * time.Millisecond)

	if healthCalls == 0 {
		t.Fatal("health check was never called")
	}
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state after failed health check = %s, want %s (should remain OPEN)", cb.GetState(), CircuitStateOpen)
	}
}

func TestCircuitBreaker_RecordFailureWithHealthCheckInOpen(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(
		1,
		time.Second,
		10*time.Millisecond,
		func() bool { return true },
		testCircuitLogger(),
	)

	cb.RecordFailure(0)
	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state = %s, want %s", cb.GetState(), CircuitStateOpen)
	}

	s := cb.GetStatus()
	if s.NextRetryTime == nil {
		t.Fatal("NextRetryTime = nil in OPEN state")
	}
}

func TestCircuitBreaker_GetStatus_ClosedHasNilNextRetry(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(2, time.Second, 10*time.Millisecond, nil, testCircuitLogger())
	s := cb.GetStatus()
	if s.State != CircuitStateClosed {
		t.Fatalf("State = %s, want %s", s.State, CircuitStateClosed)
	}
	if s.NextRetryTime != nil {
		t.Fatalf("NextRetryTime = %v, want nil in CLOSED state", s.NextRetryTime)
	}
}
