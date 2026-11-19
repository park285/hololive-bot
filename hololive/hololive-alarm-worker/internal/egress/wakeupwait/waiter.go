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

type Result string

const (
	Consumed Result = "consumed"
	Timeout  Result = "timeout"
	Failed   Result = "error"
)

type Observer interface {
	SignalWait(result Result, elapsed time.Duration)
	PollSleep(afterFailure bool, elapsed time.Duration)
}

type Config struct {
	Queue        string
	PollInterval time.Duration
	BackoffMin   time.Duration
	BackoffMax   time.Duration
}

type Waiter struct {
	cache       cache.LowLevelCache
	config      Config
	observer    Observer
	logger      *slog.Logger
	currentWait time.Duration
	sleep       func(context.Context, time.Duration) bool
}

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
