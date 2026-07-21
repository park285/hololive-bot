package pgxutil

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contextKey string

type recordingRollbacker struct {
	ctxErr      error
	deadline    time.Time
	hasDeadline bool
	traceValue  any
	err         error
}

func (r *recordingRollbacker) Rollback(ctx context.Context) error {
	r.ctxErr = ctx.Err()
	r.deadline, r.hasDeadline = ctx.Deadline()
	r.traceValue = ctx.Value(contextKey("trace"))
	return r.err
}

func TestRollbackDetachesCanceledContextAndPreservesValues(t *testing.T) {
	parent := context.WithValue(context.Background(), contextKey("trace"), "request-123")
	parent, cancel := context.WithCancel(parent)
	cancel()

	tx := &recordingRollbacker{}
	if err := Rollback(parent, tx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if tx.ctxErr != nil {
		t.Fatalf("rollback context was canceled during the driver call: %v", tx.ctxErr)
	}
	if tx.traceValue != "request-123" {
		t.Fatalf("rollback context value = %v, want request-123", tx.traceValue)
	}
	if !tx.hasDeadline {
		t.Fatal("rollback context has no deadline")
	}
	wantDeadline := time.Now().Add(rollbackTimeout)
	if delta := tx.deadline.Sub(wantDeadline); delta < -time.Second || delta > time.Second {
		t.Fatalf("rollback deadline = %v, want within one second of %v", tx.deadline, wantDeadline)
	}
}

func TestRollbackReturnsDriverError(t *testing.T) {
	want := errors.New("rollback failed")
	tx := &recordingRollbacker{err: want}

	if err := Rollback(context.Background(), tx); !errors.Is(err, want) {
		t.Fatalf("Rollback() error = %v, want %v", err, want)
	}
}

func TestRollbackAllowsNilTransaction(t *testing.T) {
	if err := Rollback(context.Background(), nil); err != nil {
		t.Fatalf("Rollback(nil) error = %v", err)
	}
}
