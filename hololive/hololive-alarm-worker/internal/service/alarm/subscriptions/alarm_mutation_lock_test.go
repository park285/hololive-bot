package subscriptions

import (
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestLockCacheMutationRecordsContendedWait(t *testing.T) {
	const operation = "lock-wait-test"

	t.Cleanup(func() { alarmMetrics().mutationLockWait.DeleteLabelValues(operation) })

	service := &AlarmService{}
	started := make(chan time.Time)

	service.cacheMutationMu.Lock()

	go func() {
		started <- service.lockCacheMutation(operation)
	}()

	if !waitForMutationLockContention(started) {
		service.cacheMutationMu.Unlock()
		t.Fatal("lockCacheMutation returned while the test still held cacheMutationMu")
	}

	for observedAt := time.Now(); time.Since(observedAt) == 0; {
		runtime.Gosched()
	}

	releasedAt := time.Now()
	service.cacheMutationMu.Unlock()

	startedAt := <-started
	service.cacheMutationMu.Unlock()

	after := mutationLockWaitSample(t, operation)

	require.True(t, startedAt.Before(releasedAt), "start time must precede lock release")
	require.Equal(t, uint64(1), after.GetSampleCount())
	require.GreaterOrEqual(t, after.GetSampleSum(), releasedAt.Sub(startedAt).Seconds())
}

func waitForMutationLockContention(started <-chan time.Time) bool {
	lockMutation := runtime.FuncForPC(reflect.ValueOf((*AlarmService).lockCacheMutation).Pointer()).Name()
	records := make([]runtime.StackRecord, runtime.NumGoroutine())

	for {
		select {
		case <-started:
			return false
		default:
		}

		n, ok := runtime.GoroutineProfile(records)
		if !ok {
			records = make([]runtime.StackRecord, n)

			continue
		}

		for i := range records[:n] {
			if mutationLockInStack(records[i].Stack(), lockMutation) {
				return true
			}
		}

		runtime.Gosched()
	}
}

func mutationLockInStack(stack []uintptr, lockMutation string) bool {
	frames := runtime.CallersFrames(stack)

	for {
		frame, more := frames.Next()
		if frame.Function == "sync.(*Mutex).Lock" {
			caller, _ := frames.Next()

			return caller.Function == lockMutation
		}

		if !more {
			return false
		}
	}
}

func mutationLockWaitSample(t *testing.T, operation string) *dto.Histogram {
	t.Helper()

	observer, err := alarmMetrics().mutationLockWait.GetMetricWithLabelValues(operation)
	require.NoError(t, err)

	metric, ok := observer.(prometheus.Metric)
	require.True(t, ok)

	var out dto.Metric

	require.NoError(t, metric.Write(&out))

	return out.GetHistogram()
}
