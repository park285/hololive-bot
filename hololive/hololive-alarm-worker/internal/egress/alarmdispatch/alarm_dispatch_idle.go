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

// WakeupConfig는 worker profile alarm_dispatch의 빈 대기 설정이다. BRPOP 상한은 BackoffMin(idle_backoff_min_ms)에서
// BackoffMax(idle_backoff_max_ms)까지 늘고, wakeup을 끄거나 BRPOP이 실패하면 PollInterval(poll_interval_ms)만큼 잠든다.
type WakeupConfig struct {
	WakeupEnabled bool
	PollInterval  time.Duration
	BackoffMin    time.Duration
	BackoffMax    time.Duration
}

// NewWakeupWaiterWithConfig는 alarm:dispatch:wakeup을 기다리는 runner 대기를 만든다. 켜진 wakeup에 cache가 없으면
// polling으로 바꾸지 않고 생성 오류로 드러낸다.
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

// wakeupObserver는 대기 결과를 alarm_dispatch_runner_* 지표로 남긴다. 지표의 wait_mode는 BRPOP 대기 wakeup, wakeup을
// 끈 대기 poll, BRPOP 실패 뒤 대기 fallback이다.
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
