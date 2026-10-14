package runtime

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
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

// 조회 실패는 마지막 깊이·경과를 현재 값처럼 갱신하지 않고 success만 내린다. 마지막 성공 시각은 유지한다.
func TestQueueObservationFailureKeepsLastValuesAndLowersSuccess(t *testing.T) {
	observedAt := time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC)

	recordQueueObservation(4, 1, 40, observedAt, nil)
	requireQueueObservationGauges(t, 4, 1, 40)
	require.InDelta(t, 1, testutil.ToFloat64(youtubeQueueSnapshotSuccess), 0)
	require.InDelta(t, float64(observedAt.Unix()), testutil.ToFloat64(youtubeQueueSnapshotLastSuccess), 0)

	recordQueueObservation(0, 0, 0, observedAt.Add(time.Minute), errors.New("queue query timed out"))
	requireQueueObservationGauges(t, 4, 1, 40)
	require.InDelta(t, 0, testutil.ToFloat64(youtubeQueueSnapshotSuccess), 0)
	require.InDelta(t, float64(observedAt.Unix()), testutil.ToFloat64(youtubeQueueSnapshotLastSuccess), 0)

	recovered := observedAt.Add(2 * time.Minute)
	recordQueueObservation(0, 0, 0, recovered, nil)
	requireQueueObservationGauges(t, 0, 0, 0)
	require.InDelta(t, 1, testutil.ToFloat64(youtubeQueueSnapshotSuccess), 0)
	require.InDelta(t, float64(recovered.Unix()), testutil.ToFloat64(youtubeQueueSnapshotLastSuccess), 0)
}

func requireQueueObservationGauges(t *testing.T, pending, processing, oldestAgeSeconds float64) {
	t.Helper()
	require.InDelta(t, pending, testutil.ToFloat64(youtubePendingQueue), 0)
	require.InDelta(t, processing, testutil.ToFloat64(youtubeProcessingQueue), 0)
	require.InDelta(t, oldestAgeSeconds, testutil.ToFloat64(youtubeQueueOldestAgeSeconds), 0)
}
