// Package wakeupwait는 alarm-worker 발송 consumer가 빈 조회 뒤 다음 조회까지 기다리는 공용 구현이다. payload 없는
// Valkey 신호 list를 BRPOP으로 기다리지만 신호는 정본이 아니므로, 신호가 없거나 Valkey가 실패해도 정해진 상한 안에
// PostgreSQL을 다시 조회한다. 신호는 hololive-shared wakeup.Signaler와 alarm dispatch publisher가 보낸다.
package wakeupwait

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/backoff"
	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// Result는 BRPOP 대기 한 번의 결과다. 값은 지표 label로 쓴다.
type Result string

const (
	Consumed Result = "consumed"
	Timeout  Result = "timeout"
	Failed   Result = "error"
)

// Observer는 대기 결과를 consumer별 지표로 남긴다. 호출자 취소로 끝난 BRPOP은 전달하지 않는다.
type Observer interface {
	// SignalWait는 BRPOP 한 번의 결과와 걸린 시간을 받는다.
	SignalWait(result Result, elapsed time.Duration)
	// PollSleep은 BRPOP 없이 PollInterval만큼 잠든 시간을 받는다. afterFailure는 BRPOP 실패 뒤의 대기다.
	PollSleep(afterFailure bool, elapsed time.Duration)
}

// Config의 시간은 모두 양수이고 BackoffMin은 BackoffMax 이하여야 한다.
type Config struct {
	// Queue는 BRPOP으로 기다릴 신호 list key다.
	Queue string
	// PollInterval은 cache가 없거나 BRPOP이 실패했을 때 잠드는 시간이다.
	PollInterval time.Duration
	// BackoffMin은 처리한 행이 있었던 뒤 첫 BRPOP 상한이다.
	BackoffMin time.Duration
	// BackoffMax는 빈 대기가 이어질 때 두 배씩 늘어나는 BRPOP 상한의 최대다.
	BackoffMax time.Duration
}

// Waiter는 신호가 오면 바로, 오지 않으면 BackoffMin부터 BackoffMax까지 늘어나는 간격으로 다시 조회하게 한다.
// 한 consumer goroutine만 쓴다.
type Waiter struct {
	cache       cache.LowLevelCache
	config      Config
	observer    Observer
	logger      *slog.Logger
	currentWait time.Duration
	sleep       func(context.Context, time.Duration) bool
}

// New는 cache가 nil이면 BRPOP 없이 PollInterval마다 조회하는 Waiter를 만든다.
func New(c cache.LowLevelCache, config Config, observer Observer, logger *slog.Logger) (*Waiter, error) {
	if config.PollInterval <= 0 || config.BackoffMin <= 0 || config.BackoffMax < config.BackoffMin {
		return nil, errors.New("new wakeup waiter: poll interval and backoff must be positive with min <= max")
	}

	if reflectutil.IsNil(c) {
		c = nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Waiter{
		cache:       c,
		config:      config,
		observer:    observer,
		logger:      logger,
		currentWait: config.BackoffMin,
		sleep:       retry.Sleep,
	}, nil
}

// Wait는 다음 조회 시점까지 기다리고, ctx가 끝나지 않았으면 true를 돌려준다.
func (w *Waiter) Wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}

	if w.cache == nil {
		return w.pollSleep(ctx, false)
	}

	startedAt := time.Now()
	consumed, err := w.waitForSignal(ctx, w.currentWait)
	elapsed := time.Since(startedAt)

	if err != nil {
		if ctx.Err() != nil {
			return false
		}

		w.observer.SignalWait(Failed, elapsed)
		w.logger.Warn("Delivery wakeup wait failed; polling",
			slog.String("queue", w.config.Queue),
			slog.Any("error", err),
		)
		w.increaseBackoff()

		return w.pollSleep(ctx, true)
	}

	if consumed {
		w.observer.SignalWait(Consumed, elapsed)
		w.Reset()
	} else {
		w.observer.SignalWait(Timeout, elapsed)
		w.increaseBackoff()
	}

	return ctx.Err() == nil
}

// Reset은 처리한 행이 있었을 때 부른다. 뒤따르는 행이 guard에 억제됐어도 다음 대기를 짧게 시작해 바로 잡는다.
func (w *Waiter) Reset() {
	w.currentWait = w.config.BackoffMin
}

func (w *Waiter) pollSleep(ctx context.Context, afterFailure bool) bool {
	startedAt := time.Now()
	ok := w.sleep(ctx, w.config.PollInterval)

	w.observer.PollSleep(afterFailure, time.Since(startedAt))

	return ok
}

func (w *Waiter) waitForSignal(ctx context.Context, timeout time.Duration) (bool, error) {
	cmd := w.cache.B().Brpop().Key(w.config.Queue).Timeout(timeout.Seconds()).Build()

	results := w.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return false, fmt.Errorf("unexpected result count: %d", len(results))
	}

	values, err := results[0].AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}

		return false, fmt.Errorf("brpop: %w", err)
	}

	return len(values) > 0, nil
}

func (w *Waiter) increaseBackoff() {
	w.currentWait = backoff.NextExponentialBackoff(w.currentWait, w.config.BackoffMax, w.config.BackoffMin)
}
