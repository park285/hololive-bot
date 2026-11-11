package alarmdispatch

import (
	"context"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// publisher가 보낸 alarm:dispatch:wakeup 신호를 바로 소비하고 기존 alarm_dispatch_runner_wakeup_* 지표로 센다.
func TestWakeupWaiterConsumesAlarmDispatchSignal(t *testing.T) {
	client, mini := sharedtestutil.NewTestCacheServiceWithMini(t.Context(), t)

	waiter, err := NewWakeupWaiterWithConfig(client, nil, WakeupConfig{
		WakeupEnabled: true,
		PollInterval:  30 * time.Second,
		BackoffMin:    250 * time.Millisecond,
		BackoffMax:    5 * time.Second,
	})
	require.NoError(t, err)

	alarmDispatchRunnerMetrics()

	consumed := testutil.ToFloat64(alarmDispatchRunnerMetrics().wakeupConsumedTotal)

	_, err = mini.Lpush(queue.AlarmDispatchWakeupQueue, "1")
	require.NoError(t, err)

	started := time.Now()

	assert.True(t, waiter.Wait(t.Context()))
	assert.Less(t, time.Since(started), 250*time.Millisecond)
	assert.False(t, mini.Exists(queue.AlarmDispatchWakeupQueue))
	assert.InDelta(t, consumed+1, testutil.ToFloat64(alarmDispatchRunnerMetrics().wakeupConsumedTotal), 0)
}

// wakeup_enabled=false면 cache가 있어도 신호를 기다리지 않고 poll_interval_ms마다 조회한다.
func TestWakeupWaiterPollsWithoutSignalWhenWakeupDisabled(t *testing.T) {
	client, mini := sharedtestutil.NewTestCacheServiceWithMini(t.Context(), t)

	waiter, err := NewWakeupWaiterWithConfig(client, nil, WakeupConfig{
		PollInterval: time.Millisecond,
		BackoffMin:   250 * time.Millisecond,
		BackoffMax:   5 * time.Second,
	})
	require.NoError(t, err)

	polls := alarmDispatchIdleWaitSampleCount(t, "poll")
	signalWaits := alarmDispatchIdleWaitSampleCount(t, "wakeup")

	_, err = mini.Lpush(queue.AlarmDispatchWakeupQueue, "1")
	require.NoError(t, err)

	assert.True(t, waiter.Wait(t.Context()))
	assert.True(t, mini.Exists(queue.AlarmDispatchWakeupQueue))
	assert.Equal(t, polls+1, alarmDispatchIdleWaitSampleCount(t, "poll"))
	assert.Equal(t, signalWaits, alarmDispatchIdleWaitSampleCount(t, "wakeup"))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.False(t, waiter.Wait(ctx))
}

// BRPOP이 실패하면 wakeup_error_total을 올리고 poll_interval_ms 대기를 wait_mode=fallback으로 센다.
func TestWakeupWaiterCountsFallbackWaitAfterSignalFailure(t *testing.T) {
	client, mini := sharedtestutil.NewTestCacheServiceWithMini(t.Context(), t)

	waiter, err := NewWakeupWaiterWithConfig(client, nil, WakeupConfig{
		WakeupEnabled: true,
		PollInterval:  time.Millisecond,
		BackoffMin:    250 * time.Millisecond,
		BackoffMax:    5 * time.Second,
	})
	require.NoError(t, err)

	wakeupErrors := testutil.ToFloat64(alarmDispatchRunnerMetrics().wakeupErrorTotal)
	fallbacks := alarmDispatchIdleWaitSampleCount(t, "fallback")
	polls := alarmDispatchIdleWaitSampleCount(t, "poll")
	signalWaits := alarmDispatchIdleWaitSampleCount(t, "wakeup")

	mini.SetError("valkey unavailable")

	assert.True(t, waiter.Wait(t.Context()))
	assert.InDelta(t, wakeupErrors+1, testutil.ToFloat64(alarmDispatchRunnerMetrics().wakeupErrorTotal), 0)
	assert.Equal(t, fallbacks+1, alarmDispatchIdleWaitSampleCount(t, "fallback"))
	assert.Equal(t, polls, alarmDispatchIdleWaitSampleCount(t, "poll"))
	assert.Equal(t, signalWaits+1, alarmDispatchIdleWaitSampleCount(t, "wakeup"))
}

func alarmDispatchIdleWaitSampleCount(t *testing.T, waitMode string) uint64 {
	t.Helper()
	alarmDispatchRunnerMetrics()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != "alarm_dispatch_runner_idle_wait_seconds" {
			continue
		}

		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "wait_mode" && label.GetValue() == waitMode {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}

	return 0
}

func TestNewWakeupWaiterWithConfigRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	waiter, err := NewWakeupWaiterWithConfig(nil, nil, WakeupConfig{
		PollInterval: 125 * time.Millisecond,
		BackoffMin:   500 * time.Millisecond,
		BackoffMax:   250 * time.Millisecond,
	})
	assert.Nil(t, waiter)
	require.ErrorContains(t, err, "build alarm dispatch wakeup waiter")

	waiter, err = NewWakeupWaiterWithConfig(nil, nil, WakeupConfig{
		WakeupEnabled: true,
		PollInterval:  time.Second,
		BackoffMin:    250 * time.Millisecond,
		BackoffMax:    time.Second,
	})
	assert.Nil(t, waiter)
	require.ErrorContains(t, err, "Valkey cache is required when wakeup is enabled")
}

func TestSleepContextReturnsTrueWhenTimerFires(t *testing.T) {
	assert.True(t, retry.Sleep(t.Context(), time.Millisecond))
}

func TestSleepContextReturnsFalseWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.False(t, retry.Sleep(ctx, time.Hour))
}
