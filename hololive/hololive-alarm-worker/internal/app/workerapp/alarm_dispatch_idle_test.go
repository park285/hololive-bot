package workerapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPGIdleWaiterConsumesWakeupAndResetsBackoff(t *testing.T) {
	waiter := &alarmDispatchWakeupWaiter{
		wakeupEnabled: true,
		pollInterval:  time.Second,
		backoffMin:    250 * time.Millisecond,
		backoffMax:    5 * time.Second,
		currentWait:   time.Second,
		waitWakeup: func(context.Context, time.Duration) (alarmDispatchWakeupWaitResult, error) {
			return alarmDispatchWakeupConsumed, nil
		},
	}

	assert.True(t, waiter.Wait(t.Context()))
	assert.Equal(t, 250*time.Millisecond, waiter.currentWait)
}

func TestPGIdleWaiterIncreasesBackoffOnTimeout(t *testing.T) {
	var timeout time.Duration
	waiter := &alarmDispatchWakeupWaiter{
		wakeupEnabled: true,
		pollInterval:  time.Second,
		backoffMin:    250 * time.Millisecond,
		backoffMax:    time.Second,
		currentWait:   250 * time.Millisecond,
		waitWakeup: func(_ context.Context, d time.Duration) (alarmDispatchWakeupWaitResult, error) {
			timeout = d
			return alarmDispatchWakeupTimeout, nil
		},
	}

	assert.True(t, waiter.Wait(t.Context()))
	assert.Equal(t, 250*time.Millisecond, timeout)
	assert.Equal(t, 500*time.Millisecond, waiter.currentWait)
}

func TestPGIdleWaiterFallsBackToPollingWhenWakeupDisabled(t *testing.T) {
	var slept time.Duration
	waiter := &alarmDispatchWakeupWaiter{
		wakeupEnabled: false,
		pollInterval:  1500 * time.Millisecond,
		sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}

	assert.True(t, waiter.Wait(t.Context()))
	assert.Equal(t, 1500*time.Millisecond, slept)
}

func TestPGIdleWaiterFallsBackToPollingWhenWakeupErrors(t *testing.T) {
	var slept time.Duration
	waiter := &alarmDispatchWakeupWaiter{
		wakeupEnabled: true,
		pollInterval:  time.Second,
		backoffMin:    250 * time.Millisecond,
		backoffMax:    5 * time.Second,
		currentWait:   250 * time.Millisecond,
		waitWakeup: func(context.Context, time.Duration) (alarmDispatchWakeupWaitResult, error) {
			return alarmDispatchWakeupTimeout, errors.New("valkey unavailable")
		},
		sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}

	assert.True(t, waiter.Wait(t.Context()))
	assert.Equal(t, time.Second, slept)
	assert.Equal(t, 500*time.Millisecond, waiter.currentWait)
}

func TestPGIdleWaiterFallsBackToPollingWhenWakeupResultUnknown(t *testing.T) {
	var slept time.Duration
	waiter := &alarmDispatchWakeupWaiter{
		wakeupEnabled: true,
		pollInterval:  2 * time.Second,
		backoffMin:    250 * time.Millisecond,
		backoffMax:    500 * time.Millisecond,
		currentWait:   250 * time.Millisecond,
		waitWakeup: func(context.Context, time.Duration) (alarmDispatchWakeupWaitResult, error) {
			return alarmDispatchWakeupWaitResult("unexpected"), nil
		},
		sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}

	assert.True(t, waiter.Wait(t.Context()))
	assert.Equal(t, 2*time.Second, slept)
	assert.Equal(t, 500*time.Millisecond, waiter.currentWait)
}

func TestPGIdleWaiterStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	waiter := &alarmDispatchWakeupWaiter{wakeupEnabled: true}

	assert.False(t, waiter.Wait(ctx))
}

func TestNewPGIdleWaiterClampsBackoffMaxToMinimum(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_WAKEUP_ENABLED", "false")
	t.Setenv("ALARM_DISPATCH_POLL_INTERVAL_MS", "125")
	t.Setenv("ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS", "500")
	t.Setenv("ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS", "250")

	waiter := newAlarmDispatchWakeupWaiter(nil, nil)

	assert.False(t, waiter.wakeupEnabled)
	assert.Equal(t, 125*time.Millisecond, waiter.pollInterval)
	assert.Equal(t, 500*time.Millisecond, waiter.backoffMin)
	assert.Equal(t, 500*time.Millisecond, waiter.backoffMax)
	assert.Equal(t, 500*time.Millisecond, waiter.currentWait)
	assert.NotNil(t, waiter.waitWakeup)
	assert.NotNil(t, waiter.sleep)
}
