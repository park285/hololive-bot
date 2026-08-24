package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/backoff"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
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

type WakeupWaiter struct {
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

func NewWakeupWaiterWithConfig(c cache.LowLevelCache, logger *slog.Logger, config WakeupConfig) (*WakeupWaiter, error) {
	if config.PollInterval <= 0 || config.BackoffMin <= 0 || config.BackoffMax < config.BackoffMin {
		return nil, errors.New("build alarm dispatch wakeup waiter: invalid polling or backoff configuration")
	}

	if config.WakeupEnabled && c == nil {
		return nil, errors.New("build alarm dispatch wakeup waiter: Valkey cache is required when wakeup is enabled")
	}

	waiter := &WakeupWaiter{
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

func (w *WakeupWaiter) Wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}

	if !w.wakeupEnabled || (w.cache == nil && w.waitWakeup == nil) {
		return w.sleepFallback(ctx, "poll")
	}

	return w.waitWithWakeup(ctx)
}

func (w *WakeupWaiter) waitWithWakeup(ctx context.Context) bool {
	waitDuration := w.currentWait
	startedAt := time.Now()
	result, err := w.waitWakeup(ctx, waitDuration)

	observeAlarmDispatchRunnerIdleWait("pg", "wakeup", time.Since(startedAt))

	if err != nil {
		return w.handleWakeupError(ctx, err)
	}

	return w.handleWakeupResult(ctx, result)
}

func (w *WakeupWaiter) handleWakeupError(ctx context.Context, err error) bool {
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

func (w *WakeupWaiter) handleWakeupResult(ctx context.Context, result alarmDispatchWakeupWaitResult) bool {
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

func (w *WakeupWaiter) Reset() {
	w.currentWait = w.backoffMin
}

func (w *WakeupWaiter) sleepFallback(ctx context.Context, waitMode string) bool {
	waitDuration := w.pollInterval
	startedAt := time.Now()
	ok := w.sleep(ctx, waitDuration)

	observeAlarmDispatchRunnerIdleWait("pg", waitMode, time.Since(startedAt))

	return ok
}

func (w *WakeupWaiter) waitForValkeyWakeup(ctx context.Context, timeout time.Duration) (alarmDispatchWakeupWaitResult, error) {
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

		return alarmDispatchWakeupTimeout, fmt.Errorf("as str slice: %w", err)
	}

	if len(values) == 0 {
		return alarmDispatchWakeupTimeout, nil
	}

	return alarmDispatchWakeupConsumed, nil
}

func (w *WakeupWaiter) increaseBackoff() {
	w.currentWait = backoff.NextExponentialBackoff(
		w.currentWait, w.backoffMax, w.backoffMin,
	)
}
