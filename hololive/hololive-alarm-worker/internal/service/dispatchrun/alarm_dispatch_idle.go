package dispatchrun

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/v2/pkg/backoff"
	"github.com/park285/shared-go/v2/pkg/retry"
)

type alarmDispatchWakeupWaitResult string

const (
	alarmDispatchWakeupConsumed alarmDispatchWakeupWaitResult = "consumed"
	alarmDispatchWakeupTimeout  alarmDispatchWakeupWaitResult = "timeout"
)

type WakeupConfig struct {
	WakeupEnabled bool
	PollInterval  time.Duration
	BackoffMin    time.Duration
	BackoffMax    time.Duration
}

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

func NewWakeupWaiterWithConfig(c cache.LowLevelCache, logger *slog.Logger, config WakeupConfig) (*alarmDispatchWakeupWaiter, error) {
	if config.PollInterval <= 0 || config.BackoffMin <= 0 || config.BackoffMax < config.BackoffMin {
		return nil, fmt.Errorf("build alarm dispatch wakeup waiter: invalid polling or backoff configuration")
	}
	if config.WakeupEnabled && c == nil {
		return nil, fmt.Errorf("build alarm dispatch wakeup waiter: Valkey cache is required when wakeup is enabled")
	}
	waiter := &alarmDispatchWakeupWaiter{
		cache:         c,
		wakeupEnabled: config.WakeupEnabled,
		pollInterval:  config.PollInterval,
		backoffMin:    config.BackoffMin,
		backoffMax:    config.BackoffMax,
		sleep:         retry.Sleep,
		logger:        logger,
	}
	waiter.currentWait = waiter.backoffMin
	waiter.waitWakeup = waiter.waitForValkeyWakeup
	return waiter, nil
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
	waitDuration := w.currentWait
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
	w.currentWait = w.backoffMin
}

func (w *alarmDispatchWakeupWaiter) sleepFallback(ctx context.Context, waitMode string) bool {
	waitDuration := w.pollInterval
	startedAt := time.Now()
	ok := w.sleep(ctx, waitDuration)
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
		w.currentWait, w.backoffMax, w.backoffMin,
	)
}
