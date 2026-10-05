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

	start := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)

	if !throttle.acquireEvery(start, queueObservationMinInterval) {
		t.Fatal("first queue observation must run")
	}

	if throttle.acquireEvery(start.Add(queueObservationMinInterval-time.Millisecond), queueObservationMinInterval) {
		t.Fatal("queue observation ran before the minimum interval elapsed")
	}

	if !throttle.acquireEvery(start.Add(queueObservationMinInterval), queueObservationMinInterval) {
		t.Fatal("queue observation did not run once the minimum interval elapsed")
	}

	if throttle.acquireEvery(start.Add(queueObservationMinInterval+time.Second), queueObservationMinInterval) {
		t.Fatal("queue observation ran again within the interval after the previous run")
	}
}

func TestQueueObservationThrottleRecoversFromBackwardClockStep(t *testing.T) {
	t.Parallel()

	var throttle queueObservationThrottle

	start := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)

	if !throttle.acquireEvery(start, queueObservationMinInterval) {
		t.Fatal("first queue observation must run")
	}

	if !throttle.acquireEvery(start.Add(-time.Hour), queueObservationMinInterval) {
		t.Fatal("backward clock step froze queue observation")
	}
}

func TestQueueObservationThrottleAdmitsOneConcurrentObserver(t *testing.T) {
	t.Parallel()

	var throttle queueObservationThrottle

	now := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)

	var (
		admitted atomic.Int64
		wg       sync.WaitGroup
	)

	for range 16 {
		wg.Go(func() {
			if throttle.acquireEvery(now, queueObservationMinInterval) {
				admitted.Add(1)
			}
		})
	}

	wg.Wait()

	if admitted.Load() != 1 {
		t.Fatalf("concurrent observers admitted = %d, want 1", admitted.Load())
	}
}
