package util

import (
	"sync"
	"testing"
	"time"
)

func newTestBreaker(threshold int, resetTimeout time.Duration) *Breaker {
	return NewBreaker(threshold, resetTimeout)
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

	// threshold - 1 회 실패
	for range threshold - 1 {
		b.RecordFailure()
	}

	// 성공 후 카운터 리셋
	b.RecordSuccess()

	// 다시 threshold - 1 회 실패해도 열리지 않아야 함
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

	// 시간 기반 reset이므로 RecordSuccess만으로는 닫히지 않음
	if !b.IsOpen() {
		t.Fatal("breaker should remain open after RecordSuccess (time-based reset only)")
	}
}

func TestBreaker_AutoResetAfterTimeout(t *testing.T) {
	b := newTestBreaker(1, 10*time.Millisecond)
	b.RecordFailure()

	if !b.IsOpen() {
		t.Fatal("breaker should be open")
	}

	time.Sleep(20 * time.Millisecond)

	if !b.Allow() {
		t.Fatal("Allow() should return true after resetTimeout")
	}

	if b.IsOpen() {
		t.Fatal("breaker should be closed after reset")
	}
}

func TestBreaker_RetryAfter(t *testing.T) {
	resetTimeout := 30 * time.Second
	b := newTestBreaker(1, resetTimeout)

	// closed 상태
	if got := b.RetryAfter(); got != 0 {
		t.Fatalf("RetryAfter() = %v, want 0 when closed", got)
	}

	b.RecordFailure()

	// open 상태: 남은 시간이 양수여야 함
	remaining := b.RetryAfter()
	if remaining <= 0 || remaining > resetTimeout {
		t.Fatalf("RetryAfter() = %v, want (0, %v]", remaining, resetTimeout)
	}
}

func TestBreaker_NilSafe(t *testing.T) {
	var b *Breaker

	if b.IsOpen() {
		t.Fatal("nil Breaker.IsOpen() should be false")
	}

	if !b.Allow() {
		t.Fatal("nil Breaker.Allow() should be true")
	}

	if got := b.RetryAfter(); got != 0 {
		t.Fatalf("nil Breaker.RetryAfter() = %v, want 0", got)
	}

	b.RecordFailure()
	b.RecordSuccess()
	b.SetOpenedAtForTest(time.Now())
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

	// 50회 실패는 threshold(100) 미만이므로 열리지 않아야 함
	if b.IsOpen() {
		t.Fatal("breaker should not be open after 50 failures with threshold 100")
	}

	// 50회 더 실패 → threshold 도달
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
	// CAS 전이 검증: threshold 경계에서 다수 goroutine이 동시에 RecordFailure해도
	// open 전이(true 반환)는 정확히 1회여야 한다(openedAt 오염·중복 전이 방지).
	b := newTestBreaker(2, 30*time.Second)
	b.RecordFailure() // failures=1 (threshold-1)

	const goroutines = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
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
	b := newTestBreaker(1, 30*time.Second)
	b.RecordFailure()

	openedAt1 := b.openedAtTime()
	if openedAt1.IsZero() {
		t.Fatal("openedAt must be set after first open")
	}

	time.Sleep(2 * time.Millisecond)

	// open 상태에서 추가 RecordFailure → openedAt 갱신 금지
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()

	openedAt2 := b.openedAtTime()
	if !openedAt1.Equal(openedAt2) {
		t.Fatalf("openedAt must not change while circuit is open: before=%v after=%v", openedAt1, openedAt2)
	}
}

func TestBreaker_AfterReset_ThresholdRequiredAgain(t *testing.T) {
	// M3 핵심: timeout 경과 후 reset → failures=0 → threshold 미달로 즉시 재open 없음
	b := newTestBreaker(3, 10*time.Millisecond)

	for range 3 {
		b.RecordFailure()
	}
	if !b.IsOpen() {
		t.Fatal("should be open after threshold failures")
	}

	time.Sleep(20 * time.Millisecond)

	// Allow()로 reset
	if !b.Allow() {
		t.Fatal("Allow() should return true after resetTimeout")
	}

	// reset 후 단 1회 실패로는 열리지 않아야 함
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("single failure after reset must not re-open (threshold=3)")
	}
}

func TestBreaker_AllowResetsOnTimeout(t *testing.T) {
	b := newTestBreaker(1, 10*time.Millisecond)
	b.RecordFailure()

	// open 상태
	if b.Allow() {
		t.Fatal("Allow() should be false immediately after open")
	}

	// resetTimeout 경과
	time.Sleep(20 * time.Millisecond)

	// Allow()가 reset 트리거 후 true를 반환해야 함
	if !b.Allow() {
		t.Fatal("Allow() should return true after resetTimeout elapsed")
	}

	// Allow() 호출로 reset됐으므로 closed 상태여야 함
	if b.IsOpen() {
		t.Fatal("breaker should be closed after Allow() triggered reset")
	}
}
