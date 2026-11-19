package subscriptions

import (
	"errors"
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
	started := make(chan time.Time, 1)
	acquire := func() { started <- service.lockCacheMutation(operation) }

	service.cacheMutationMu.Lock()

	go acquire()

	if err := waitForMutationLockContention(started, acquire); err != nil {
		service.cacheMutationMu.Unlock()
		<-started
		t.Fatal(err)
	}

	whileLocked := mutationLockWaitSample(t, operation)
	releasedAt := time.Now()
	service.cacheMutationMu.Unlock()

	startedAt := <-started
	service.cacheMutationMu.Unlock()

	after := mutationLockWaitSample(t, operation)

	require.Zero(t, whileLocked.GetSampleCount(), "wait recorded before acquiring cacheMutationMu")
	require.False(t, startedAt.After(releasedAt), "start time must not follow lock release")
	require.Equal(t, uint64(1), after.GetSampleCount())
	require.GreaterOrEqual(t, after.GetSampleSum(), releasedAt.Sub(startedAt).Seconds())
}

func waitForMutationLockContention(started <-chan time.Time, acquire func()) error {
	worker := runtime.FuncForPC(reflect.ValueOf(acquire).Pointer()).Name()
	lockMutation := runtime.FuncForPC(reflect.ValueOf((*AlarmService).lockCacheMutation).Pointer()).Name()
	records := make([]runtime.StackRecord, runtime.NumGoroutine())

	for {
		n, ok := runtime.GoroutineProfile(records)
		if !ok {
			records = make([]runtime.StackRecord, n)

			continue
		}

		visible := false

		for i := range records[:n] {
			waiting, found := mutationLockInStack(records[i].Stack(), lockMutation, worker)
			if waiting {
				return nil
			}

			visible = visible || found
		}

		if !visible {
			if len(started) != 0 {
				return errors.New("lockCacheMutation returned while the test still held cacheMutationMu")
			}

			return errors.New("goroutine profile truncated before the lockCacheMutation worker; increase GODEBUG=profstackdepth")
		}

		runtime.Gosched()
	}
}

func mutationLockInStack(stack []uintptr, lockMutation, worker string) (waiting, visible bool) {
	frames := runtime.CallersFrames(stack)

	var previous string

	for {
		frame, more := frames.Next()
		if frame.Function == lockMutation && previous == "sync.(*Mutex).Lock" {
			return true, true
		}

		if frame.Function == worker {
			return false, true
		}

		if !more {
			return false, false
		}

		previous = frame.Function
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
