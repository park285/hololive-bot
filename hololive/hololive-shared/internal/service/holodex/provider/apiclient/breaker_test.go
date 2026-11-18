package apiclient

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func newTestBreaker(threshold int, resetTimeout time.Duration) *circuitBreaker {
	return newCircuitBreaker(threshold, resetTimeout)
}

func TestBreaker_ClosedByDefault(t *testing.T) {
	b := newTestBreaker(3, 30*time.Second)

	if b.IsOpen() {
		t.Fatal("breaker should be closed by default")
	}

	if !b.Allow() {
		t.Fatal("Allow() should return true when breaker is closed")
	}
}

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	threshold := 3
	b := newTestBreaker(threshold, 30*time.Second)

	for i := range threshold {
		if b.IsOpen() {
			t.Fatalf("breaker opened after %d failures (before threshold %d)", i, threshold)
		}

		b.RecordFailure()
	}

	if !b.IsOpen() {
		t.Fatalf("breaker should be open after %d failures", threshold)
	}

	if b.Allow() {
		t.Fatal("Allow() should return false when breaker is open")
	}
}

func TestBreaker_RecordSuccess_ResetsCounter(t *testing.T) {
	threshold := 3
	b := newTestBreaker(threshold, 30*time.Second)

	for range threshold - 1 {
		b.RecordFailure()
	}

	b.RecordSuccess()

	for range threshold - 1 {
		b.RecordFailure()
	}

	if b.IsOpen() {
		t.Fatal("breaker should not be open after RecordSuccess reset the counter")
	}
}

func TestBreaker_RecordSuccess_DoesNotCloseOpenBreaker(t *testing.T) {
	b := newTestBreaker(1, 30*time.Second)
	b.RecordFailure()

	if !b.IsOpen() {
		t.Fatal("breaker should be open")
	}

	b.RecordSuccess()

	if !b.IsOpen() {
		t.Fatal("breaker should remain open after RecordSuccess (time-based reset only)")
	}
}

func TestBreaker_AutoResetAfterTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(1, 10*time.Millisecond)
		b.RecordFailure()

		if !b.IsOpen() {
			t.Fatal("breaker should be open")
		}

		synctest.Sleep(10 * time.Millisecond)

		if b.Allow() {
			t.Fatal("Allow() should remain false at resetTimeout")
		}

		synctest.Sleep(time.Nanosecond)

		if !b.Allow() {
			t.Fatal("Allow() should return true after resetTimeout")
		}

		if b.IsOpen() {
			t.Fatal("breaker should be closed after reset")
		}
	})
}

func TestBreaker_RetryAfter(t *testing.T) {
	resetTimeout := 30 * time.Second
	b := newTestBreaker(1, resetTimeout)

	if got := b.RetryAfter(); got != 0 {
		t.Fatalf("RetryAfter() = %v, want 0 when closed", got)
	}

	b.RecordFailure()

	remaining := b.RetryAfter()
	if remaining <= 0 || remaining > resetTimeout {
		t.Fatalf("RetryAfter() = %v, want (0, %v]", remaining, resetTimeout)
	}
}

func TestBreaker_NilSafe(t *testing.T) {
	var b *circuitBreaker

	if b.IsOpen() {
		t.Fatal("nil circuitBreaker.IsOpen() should be false")
	}

	if !b.Allow() {
		t.Fatal("nil circuitBreaker.Allow() should be true")
	}

	if got := b.RetryAfter(); got != 0 {
		t.Fatalf("nil circuitBreaker.RetryAfter() = %v, want 0", got)
	}

	b.RecordFailure()
	b.RecordSuccess()
}

func TestBreaker_ConcurrentRecordFailure(t *testing.T) {
	b := newTestBreaker(100, 30*time.Second)

	const goroutines = 50

	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			b.RecordFailure()
		})
	}

	wg.Wait()

	if b.IsOpen() {
		t.Fatal("breaker should not be open after 50 failures with threshold 100")
	}

	for range goroutines {
		wg.Go(func() {
			b.RecordFailure()
		})
	}

	wg.Wait()

	if !b.IsOpen() {
		t.Fatal("breaker should be open after 100 total failures with threshold 100")
	}
}

func TestBreaker_RecordFailure_ConcurrentTransitionOpensOnce(t *testing.T) {
	b := newTestBreaker(2, 30*time.Second)
	b.RecordFailure()

	const goroutines = 64

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	openedCount := 0

	for range goroutines {
		wg.Go(func() {
			if b.RecordFailure() {
				mu.Lock()

				openedCount++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if openedCount != 1 {
		t.Errorf("expected exactly 1 open transition under concurrency, got %d", openedCount)
	}

	if !b.IsOpen() {
		t.Error("breaker should be open after concurrent threshold failures")
	}
}

func TestBreaker_RecordFailure_OpenedAtFixedOnceOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(1, 30*time.Second)
		b.RecordFailure()

		openedAt1 := b.openedAtTime()
		if openedAt1.IsZero() {
			t.Fatal("openedAt must be set after first open")
		}

		synctest.Sleep(2 * time.Millisecond)

		b.RecordFailure()
		b.RecordFailure()
		b.RecordFailure()

		openedAt2 := b.openedAtTime()
		if !openedAt1.Equal(openedAt2) {
			t.Fatalf("openedAt must not change while circuit is open: before=%v after=%v", openedAt1, openedAt2)
		}
	})
}

func TestBreaker_AfterReset_ThresholdRequiredAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(3, 10*time.Millisecond)

		for range 3 {
			b.RecordFailure()
		}

		if !b.IsOpen() {
			t.Fatal("should be open after threshold failures")
		}

		synctest.Sleep(20 * time.Millisecond)

		if !b.Allow() {
			t.Fatal("Allow() should return true after resetTimeout")
		}

		b.RecordFailure()

		if b.IsOpen() {
			t.Fatal("single failure after reset must not re-open (threshold=3)")
		}
	})
}

func TestBreaker_AllowResetsOnTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(1, 10*time.Millisecond)
		b.RecordFailure()

		if b.Allow() {
			t.Fatal("Allow() should be false immediately after open")
		}

		synctest.Sleep(20 * time.Millisecond)

		if !b.Allow() {
			t.Fatal("Allow() should return true after resetTimeout elapsed")
		}

		if b.IsOpen() {
			t.Fatal("breaker should be closed after Allow() triggered reset")
		}
	})
}

func (b *circuitBreaker) SetOpenedAtForTest(t time.Time) {
	b.openedAt.Store(t)
}
