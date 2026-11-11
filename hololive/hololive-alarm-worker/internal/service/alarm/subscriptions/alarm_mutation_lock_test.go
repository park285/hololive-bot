package subscriptions

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestLockCacheMutationRecordsContendedWait(t *testing.T) {
	const (
		operation = "lock-wait-test"
		hold      = 30 * time.Millisecond
	)

	service := &AlarmService{}
	before := mutationLockWaitSample(t, operation)

	service.cacheMutationMu.Lock()

	released := make(chan struct{})

	go func() {
		time.Sleep(hold)
		service.cacheMutationMu.Unlock()
		close(released)
	}()

	startedAt := service.lockCacheMutation(operation)
	waited := time.Since(startedAt)

	service.cacheMutationMu.Unlock()
	<-released

	after := mutationLockWaitSample(t, operation)

	require.GreaterOrEqual(t, waited, hold, "작업 시작 시각은 lock 대기 전이어야 한다")
	require.Equal(t, before.GetSampleCount()+1, after.GetSampleCount())
	require.GreaterOrEqual(t, after.GetSampleSum()-before.GetSampleSum(), hold.Seconds())
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
