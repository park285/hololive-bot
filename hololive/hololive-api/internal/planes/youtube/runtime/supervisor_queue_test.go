package runtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueObservationThrottleSkipsWithinMinInterval(t *testing.T) {
	t.Parallel()
	var throttle queueObservationThrottle
	start := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	if !throttle.acquire(start) {
		t.Fatal("first queue observation must run")
	}
	if throttle.acquire(start.Add(queueObservationMinInterval - time.Millisecond)) {
		t.Fatal("queue observation ran before the minimum interval elapsed")
	}
	if !throttle.acquire(start.Add(queueObservationMinInterval)) {
		t.Fatal("queue observation did not run once the minimum interval elapsed")
	}
	if throttle.acquire(start.Add(queueObservationMinInterval + time.Second)) {
		t.Fatal("queue observation ran again within the interval after the previous run")
	}
}

func TestQueueObservationThrottleRecoversFromBackwardClockStep(t *testing.T) {
	t.Parallel()
	var throttle queueObservationThrottle
	start := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	if !throttle.acquire(start) {
		t.Fatal("first queue observation must run")
	}
	if !throttle.acquire(start.Add(-time.Hour)) {
		t.Fatal("backward clock step froze queue observation")
	}
}

func TestQueueObservationThrottleAdmitsOneConcurrentObserver(t *testing.T) {
	t.Parallel()
	var throttle queueObservationThrottle
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if throttle.acquire(now) {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("concurrent observers admitted = %d, want 1", admitted.Load())
	}
}
