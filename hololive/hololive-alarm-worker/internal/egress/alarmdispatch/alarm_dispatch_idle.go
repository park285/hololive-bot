package alarmdispatch

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/wakeupwait"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type WakeupConfig struct {
	WakeupEnabled bool
	PollInterval  time.Duration
	BackoffMin    time.Duration
	BackoffMax    time.Duration
}

func NewWakeupWaiterWithConfig(c cache.LowLevelCache, logger *slog.Logger, config WakeupConfig) (*wakeupwait.Waiter, error) {
	if !config.WakeupEnabled {
		c = nil
	} else if reflectutil.IsNil(c) {
		return nil, errors.New("build alarm dispatch wakeup waiter: Valkey cache is required when wakeup is enabled")
	}

	waiter, err := wakeupwait.New(c, wakeupwait.Config{
		Queue:        queue.AlarmDispatchWakeupQueue,
		PollInterval: config.PollInterval,
		BackoffMin:   config.BackoffMin,
		BackoffMax:   config.BackoffMax,
	}, wakeupObserver{}, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm dispatch wakeup waiter: %w", err)
	}

	return waiter, nil
}

type wakeupObserver struct{}

func (wakeupObserver) SignalWait(result wakeupwait.Result, elapsed time.Duration) {
	observeAlarmDispatchRunnerIdleWait("wakeup", elapsed)

	switch result {
	case wakeupwait.Consumed:
		observeAlarmDispatchRunnerWakeupConsumed()
	case wakeupwait.Timeout:
		observeAlarmDispatchRunnerWakeupTimeout()
	case wakeupwait.Failed:
		observeAlarmDispatchRunnerWakeupError()
	}
}

func (wakeupObserver) PollSleep(afterFailure bool, elapsed time.Duration) {
	waitMode := "poll"

	if afterFailure {
		waitMode = "fallback"
	}

	observeAlarmDispatchRunnerIdleWait(waitMode, elapsed)
}
