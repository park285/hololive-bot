package cleanupctx

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contextKey string

func TestWithTimeoutDetachesCancellationAndPreservesValues(t *testing.T) {
	parent := context.WithValue(t.Context(), contextKey("trace"), "trace-1")
	parent, cancelParent := context.WithCancel(parent)
	cancelParent()

	ctx, cancel := WithTimeout(parent, 100*time.Millisecond)
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("cleanup context starts canceled: %v", err)
	}

	if got := ctx.Value(contextKey("trace")); got != "trace-1" {
		t.Fatalf("cleanup context value = %v, want trace-1", got)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("cleanup context has no deadline")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 150*time.Millisecond {
		t.Fatalf("cleanup deadline remaining = %v, want (0, 150ms]", remaining)
	}
}

func TestWithTimeoutUsesDefaultForNonPositiveTimeout(t *testing.T) {
	ctx, cancel := WithTimeout(t.Context(), 0)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("cleanup context has no deadline")
	}

	remaining := time.Until(deadline)
	if remaining < DefaultTimeout-time.Second || remaining > DefaultTimeout+time.Second {
		t.Fatalf("cleanup deadline remaining = %v, want about %v", remaining, DefaultTimeout)
	}
}

func TestWithTimeoutAcceptsNilParent(t *testing.T) {
	//lint:ignore SA1012 nil parent를 background context로 바꾸는 명시적 계약을 검증한다.
	ctx, cancel := WithTimeout(nil, time.Second) //nolint:staticcheck // nil parent fallback 계약을 직접 검증한다.
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("cleanup context error = %v, want nil", err)
	}
}

func TestWaitReturnsWhenDoneCloses(t *testing.T) {
	done := make(chan struct{})
	close(done)

	if err := Wait(t.Context(), time.Second, done); err != nil {
		t.Fatalf("Wait() error = %v, want nil", err)
	}
}

func TestWaitTimesOutEvenWhenParentIsAlreadyCanceled(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())
	cancelParent()

	err := Wait(parent, 15*time.Millisecond, make(chan struct{}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want deadline exceeded", err)
	}
}

func TestWaitRejectsNilDoneChannel(t *testing.T) {
	if err := Wait(t.Context(), time.Second, nil); !errors.Is(err, ErrNilDone) {
		t.Fatalf("Wait(nil) error = %v, want ErrNilDone", err)
	}
}

func TestWaitAcceptsNilParent(t *testing.T) {
	done := make(chan struct{})
	close(done)

	//lint:ignore SA1012 nil parent를 background context로 바꾸는 명시적 계약을 검증한다.
	if err := Wait(nil, time.Second, done); err != nil { //nolint:staticcheck // nil parent fallback 계약을 직접 검증한다.
		t.Fatalf("Wait(nil parent) error = %v, want nil", err)
	}
}
