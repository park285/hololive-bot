package schedulerkit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type stubLocker struct {
	mu              sync.Mutex
	acquireToken    string
	acquireAcquired bool
	acquireErr      error
	releaseCalls    []string
}

func (s *stubLocker) TryAcquire(_ context.Context, _ string, _ time.Duration) (string, bool, error) {
	return s.acquireToken, s.acquireAcquired, s.acquireErr
}

func (s *stubLocker) Release(_ context.Context, lockKey, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls = append(s.releaseCalls, lockKey)
	return nil
}

func (s *stubLocker) ClaimRoom(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (s *stubLocker) ReleaseRoomClaims(_ context.Context, _ []string) error {
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDigestScheduler_SetClockAndClock(t *testing.T) {
	ds := NewDigestScheduler(nil, discardLogger())
	want := time.Date(2026, 5, 25, 9, 0, 0, 0, time.UTC)

	ds.SetClock(func() time.Time { return want })

	if got := ds.Clock(); !got.Equal(want) {
		t.Fatalf("Clock() = %s, want %s", got, want)
	}
}

func TestDigestScheduler_NilReceiverSafety(t *testing.T) {
	var ds *DigestScheduler
	ds.SetClock(func() time.Time { return time.Now() })
	ds.Stop()

	got := ds.Clock()
	if got.IsZero() {
		t.Fatal("nil DigestScheduler.Clock() should return current time, not zero")
	}
}

func TestRunDigest_HappyPath(t *testing.T) {
	locker := &stubLocker{acquireToken: "tok", acquireAcquired: true}
	ds := NewDigestScheduler(locker, discardLogger())

	type collected struct{ items []string }
	var executedWith collected

	err := RunDigest(context.Background(), ds, DigestOp[collected]{
		LockKey: "test:lock:2026-01",
		OnLockNotAcquired: func() error {
			t.Fatal("should not be called on happy path")
			return nil
		},
		Collect: func(_ context.Context) (collected, bool, error) {
			return collected{items: []string{"a", "b"}}, true, nil
		},
		Execute: func(_ context.Context, c collected) error {
			executedWith = c
			return nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(executedWith.items) != 2 {
		t.Fatalf("Execute received %d items, want 2", len(executedWith.items))
	}
	if len(locker.releaseCalls) != 1 || locker.releaseCalls[0] != "test:lock:2026-01" {
		t.Fatalf("expected 1 Release call for 'test:lock:2026-01', got %v", locker.releaseCalls)
	}
}

func TestRunDigest_LockAcquireError(t *testing.T) {
	locker := &stubLocker{acquireErr: errors.New("redis down")}
	ds := NewDigestScheduler(locker, discardLogger())

	err := RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock",
		Collect: func(_ context.Context) (struct{}, bool, error) {
			t.Fatal("Collect should not be called when lock acquire fails")
			return struct{}{}, false, nil
		},
		Execute: func(_ context.Context, _ struct{}) error {
			t.Fatal("Execute should not be called when lock acquire fails")
			return nil
		},
	})

	if err == nil {
		t.Fatal("expected error when lock acquisition fails")
	}
}

func TestRunDigest_LockNotAcquired_ReturnError(t *testing.T) {
	sentinel := errors.New("notification in progress")
	locker := &stubLocker{acquireAcquired: false}
	ds := NewDigestScheduler(locker, discardLogger())

	err := RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock",
		OnLockNotAcquired: func() error {
			return sentinel
		},
		Collect: func(_ context.Context) (struct{}, bool, error) {
			t.Fatal("Collect should not be called when lock not acquired")
			return struct{}{}, false, nil
		},
		Execute: func(_ context.Context, _ struct{}) error {
			t.Fatal("Execute should not be called when lock not acquired")
			return nil
		},
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

func TestRunDigest_LockNotAcquired_ReturnNil(t *testing.T) {
	locker := &stubLocker{acquireAcquired: false}
	ds := NewDigestScheduler(locker, discardLogger())

	err := RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock",
		OnLockNotAcquired: func() error {
			return nil
		},
		Collect: func(_ context.Context) (struct{}, bool, error) {
			t.Fatal("Collect should not be called")
			return struct{}{}, false, nil
		},
		Execute: func(_ context.Context, _ struct{}) error {
			t.Fatal("Execute should not be called")
			return nil
		},
	})

	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestRunDigest_CollectReturnsFalse_SkipsExecute(t *testing.T) {
	locker := &stubLocker{acquireToken: "tok", acquireAcquired: true}
	ds := NewDigestScheduler(locker, discardLogger())

	err := RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock",
		Collect: func(_ context.Context) (struct{}, bool, error) {
			return struct{}{}, false, nil
		},
		Execute: func(_ context.Context, _ struct{}) error {
			t.Fatal("Execute should not be called when Collect returns false")
			return nil
		},
	})

	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(locker.releaseCalls) != 1 {
		t.Fatal("lock should still be released when collect returns false")
	}
}

func TestRunDigest_CollectError_Propagates(t *testing.T) {
	locker := &stubLocker{acquireToken: "tok", acquireAcquired: true}
	ds := NewDigestScheduler(locker, discardLogger())
	collectErr := errors.New("db connection failed")

	err := RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock",
		Collect: func(_ context.Context) (struct{}, bool, error) {
			return struct{}{}, false, collectErr
		},
		Execute: func(_ context.Context, _ struct{}) error {
			t.Fatal("Execute should not be called on collect error")
			return nil
		},
	})

	if !errors.Is(err, collectErr) {
		t.Fatalf("expected collect error, got: %v", err)
	}
	if len(locker.releaseCalls) != 1 {
		t.Fatal("lock should be released even on collect error")
	}
}

func TestRunDigest_ExecuteError_Propagates(t *testing.T) {
	locker := &stubLocker{acquireToken: "tok", acquireAcquired: true}
	ds := NewDigestScheduler(locker, discardLogger())
	execErr := errors.New("enqueue failed")

	err := RunDigest(context.Background(), ds, DigestOp[int]{
		LockKey: "test:lock",
		Collect: func(_ context.Context) (int, bool, error) {
			return 42, true, nil
		},
		Execute: func(_ context.Context, n int) error {
			if n != 42 {
				t.Fatalf("Execute received %d, want 42", n)
			}
			return execErr
		},
	})

	if !errors.Is(err, execErr) {
		t.Fatalf("expected execute error, got: %v", err)
	}
}

func TestRunDigest_LockReleasedOnExecuteError(t *testing.T) {
	locker := &stubLocker{acquireToken: "tok", acquireAcquired: true}
	ds := NewDigestScheduler(locker, discardLogger())

	_ = RunDigest(context.Background(), ds, DigestOp[struct{}]{
		LockKey: "test:lock:key",
		Collect: func(_ context.Context) (struct{}, bool, error) {
			return struct{}{}, true, nil
		},
		Execute: func(_ context.Context, _ struct{}) error {
			return errors.New("boom")
		},
	})

	if len(locker.releaseCalls) != 1 || locker.releaseCalls[0] != "test:lock:key" {
		t.Fatalf("lock should be released on execute error, got release calls: %v", locker.releaseCalls)
	}
}

func TestNewDigestScheduler_NilLocker_UsesNoop(t *testing.T) {
	ds := NewDigestScheduler(nil, discardLogger())
	if ds.Locker == nil {
		t.Fatal("Locker should not be nil after NewDigestScheduler(nil, ...)")
	}

	token, acquired, err := ds.Locker.TryAcquire(context.Background(), "k", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("noop locker should always acquire, got token=%q acquired=%v err=%v", token, acquired, err)
	}
}

func TestNewDigestScheduler_NilLogger_UsesDefault(t *testing.T) {
	ds := NewDigestScheduler(nil, nil)
	if ds.Logger == nil {
		t.Fatal("Logger should not be nil after NewDigestScheduler(..., nil)")
	}
}

func TestDigestScheduler_StartStop(t *testing.T) {
	ds := NewDigestScheduler(nil, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	tickCalled := make(chan struct{}, 1)

	ds.Start(ctx, Config{
		Logger:         discardLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "ctx stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(-time.Millisecond)
		},
		OnTick: func(context.Context) {
			select {
			case tickCalled <- struct{}{}:
			default:
			}
			cancel()
		},
	})

	select {
	case <-tickCalled:
	case <-time.After(time.Second):
		t.Fatal("expected tick to be called")
	}

	ds.Stop()
}

func TestShouldMark_AllSuccess(t *testing.T) {
	result := delivery.SendResult{Attempted: 3, Sent: 3}
	shouldMark, err := ShouldMark(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldMark {
		t.Fatal("expected shouldMark=true for all success")
	}
}

func TestShouldMark_AllFailed(t *testing.T) {
	result := delivery.SendResult{Attempted: 2, Failed: 2, FailedRooms: []string{"r1", "r2"}}
	shouldMark, err := ShouldMark(result)
	if err == nil {
		t.Fatal("expected error for all-failed")
	}
	if shouldMark {
		t.Fatal("expected shouldMark=false for all-failed")
	}
}

func TestShouldMark_PartialFailure(t *testing.T) {
	result := delivery.SendResult{Attempted: 3, Sent: 2, Failed: 1, FailedRooms: []string{"r3"}}
	shouldMark, err := ShouldMark(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldMark {
		t.Fatal("expected shouldMark=false for partial failure")
	}
}
