package workerapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/backoff"
)

type alarmDispatchWakeupWaitResult string

const (
	alarmDispatchWakeupConsumed alarmDispatchWakeupWaitResult = "consumed"
	alarmDispatchWakeupTimeout  alarmDispatchWakeupWaitResult = "timeout"
)

type alarmDispatchWakeupWaiter struct {
	cache         cache.LowLevelCache
	wakeupEnabled bool
	pollInterval  time.Duration
	backoffMin    time.Duration
	backoffMax    time.Duration
	currentWait   time.Duration
	waitWakeup    func(context.Context, time.Duration) (alarmDispatchWakeupWaitResult, error)
	sleep         func(context.Context, time.Duration) bool
	logger        *slog.Logger
}

func newAlarmDispatchWakeupWaiter(c cache.LowLevelCache, logger *slog.Logger) *alarmDispatchWakeupWaiter {
	waiter := &alarmDispatchWakeupWaiter{
		cache:         c,
		wakeupEnabled: parseBoolEnv("ALARM_DISPATCH_WAKEUP_ENABLED", true),
		pollInterval:  parsePositiveDurationMSEnv("ALARM_DISPATCH_POLL_INTERVAL_MS", time.Second),
		backoffMin:    parsePositiveDurationMSEnv("ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS", 250*time.Millisecond),
		backoffMax:    parsePositiveDurationMSEnv("ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS", 5*time.Second),
		sleep:         sleepContext,
		logger:        logger,
	}
	if waiter.backoffMax < waiter.backoffMin {
		waiter.backoffMax = waiter.backoffMin
	}
	waiter.currentWait = waiter.backoffMin
	waiter.waitWakeup = waiter.waitForValkeyWakeup
	return waiter
}

func (w *alarmDispatchWakeupWaiter) Wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if !w.wakeupEnabled || (w.cache == nil && w.waitWakeup == nil) {
		return w.sleepFallback(ctx, "poll")
	}
	return w.waitWithWakeup(ctx)
}

func (w *alarmDispatchWakeupWaiter) waitWithWakeup(ctx context.Context) bool {
	waitDuration := w.effectiveCurrentWait()
	startedAt := time.Now()
	result, err := w.waitWakeup(ctx, waitDuration)
	observeAlarmDispatchRunnerIdleWait("pg", "wakeup", time.Since(startedAt))
	if err != nil {
		return w.handleWakeupError(ctx, err)
	}
	return w.handleWakeupResult(ctx, result)
}

func (w *alarmDispatchWakeupWaiter) handleWakeupError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	observeAlarmDispatchRunnerWakeupError()
	if w.logger != nil {
		w.logger.Warn("Alarm dispatch wakeup wait failed", slog.Any("error", err))
	}
	w.increaseBackoff()
	return w.sleepFallback(ctx, "fallback")
}

func (w *alarmDispatchWakeupWaiter) handleWakeupResult(ctx context.Context, result alarmDispatchWakeupWaitResult) bool {
	switch result {
	case alarmDispatchWakeupConsumed:
		observeAlarmDispatchRunnerWakeupConsumed()
		w.Reset()
		return ctx.Err() == nil
	case alarmDispatchWakeupTimeout:
		observeAlarmDispatchRunnerWakeupTimeout()
		w.increaseBackoff()
		return ctx.Err() == nil
	default:
		observeAlarmDispatchRunnerWakeupError()
		w.increaseBackoff()
		return w.sleepFallback(ctx, "fallback")
	}
}

func (w *alarmDispatchWakeupWaiter) Reset() {
	w.currentWait = w.effectiveBackoffMin()
}

func (w *alarmDispatchWakeupWaiter) sleepFallback(ctx context.Context, waitMode string) bool {
	waitDuration := w.effectivePollInterval()
	startedAt := time.Now()
	ok := w.effectiveSleep()(ctx, waitDuration)
	observeAlarmDispatchRunnerIdleWait("pg", waitMode, time.Since(startedAt))
	return ok
}

func (w *alarmDispatchWakeupWaiter) waitForValkeyWakeup(ctx context.Context, timeout time.Duration) (alarmDispatchWakeupWaitResult, error) {
	cmd := w.cache.B().Brpop().Key(queue.AlarmDispatchWakeupQueue).Timeout(timeout.Seconds()).Build()
	results := w.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return alarmDispatchWakeupTimeout, fmt.Errorf("unexpected result count: %d", len(results))
	}
	values, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return alarmDispatchWakeupTimeout, nil
		}
		return alarmDispatchWakeupTimeout, err
	}
	if len(values) == 0 {
		return alarmDispatchWakeupTimeout, nil
	}
	return alarmDispatchWakeupConsumed, nil
}

func (w *alarmDispatchWakeupWaiter) increaseBackoff() {
	w.currentWait = backoff.NextExponentialBackoff(
		w.effectiveCurrentWait(), w.effectiveBackoffMax(), w.effectiveBackoffMin(),
	)
}

func (w *alarmDispatchWakeupWaiter) effectiveCurrentWait() time.Duration {
	if w.currentWait > 0 {
		return w.currentWait
	}
	return w.effectiveBackoffMin()
}

func (w *alarmDispatchWakeupWaiter) effectiveBackoffMin() time.Duration {
	if w.backoffMin > 0 {
		return w.backoffMin
	}
	return 250 * time.Millisecond
}

func (w *alarmDispatchWakeupWaiter) effectiveBackoffMax() time.Duration {
	if w.backoffMax > 0 {
		return w.backoffMax
	}
	return 5 * time.Second
}

func (w *alarmDispatchWakeupWaiter) effectivePollInterval() time.Duration {
	if w.pollInterval > 0 {
		return w.pollInterval
	}
	return time.Second
}

func (w *alarmDispatchWakeupWaiter) effectiveSleep() func(context.Context, time.Duration) bool {
	if w.sleep != nil {
		return w.sleep
	}
	return sleepContext
}
