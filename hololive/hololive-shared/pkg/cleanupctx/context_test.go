package cleanupctx

import (
	"context"
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
