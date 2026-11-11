package wakeup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/backoff"
	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// 이 값은 alarm dispatch 프로필의 idle_backoff_min_ms와 같고, guard(3s)가 억제한 행을 250ms·500ms·1s·2s 재조회가 guard 창 안에 잡는다.
const initialBackoff = 250 * time.Millisecond

var waitResults = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_delivery_wakeup_waits_total",
		Help: "Delivery consumer idle waits by channel and result (consumed, timeout, error).",
	}, []string{"channel", "result"})
})

// Waiter는 consumer가 빈 조회 뒤 다음 조회까지 기다리는 방법이다. 신호가 오면 바로, 오지 않으면 250ms부터 poll
// 주기까지 늘어나는 간격으로 다시 조회한다. 연결된 cache가 없으면 poll 주기만큼 잠들어 wakeup 이전과 같은 주기로 조회하고,
// BRPOP이 실패해도 poll 주기만큼 잠든다. 한 consumer goroutine만 쓴다.
type Waiter struct {
	cache        cache.LowLevelCache
	channel      Channel
	pollInterval time.Duration
	backoffMin   time.Duration
	currentWait  time.Duration
	logger       *slog.Logger
	sleep        func(context.Context, time.Duration) bool
}

// NewWaiter의 pollInterval은 consumer 프로필의 poll_interval_ms다. 신호 대기 상한과 Valkey 오류 때 대기로 쓴다.
func NewWaiter(c cache.LowLevelCache, channel Channel, pollInterval time.Duration, logger *slog.Logger) (*Waiter, error) {
	if pollInterval <= 0 {
		return nil, errors.New("new delivery wakeup waiter: poll interval must be positive")
	}

	if reflectutil.IsNil(c) {
		c = nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	minimum := min(initialBackoff, pollInterval)

	return &Waiter{
		cache:        c,
		channel:      channel,
		pollInterval: pollInterval,
		backoffMin:   minimum,
		currentWait:  minimum,
		logger:       logger,
		sleep:        retry.Sleep,
	}, nil
}

func (w *Waiter) Wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}

	if w.cache == nil {
		return w.sleep(ctx, w.pollInterval)
	}

	consumed, err := w.waitForSignal(ctx, w.currentWait)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}

		w.observe("error")
		w.logger.Warn("Delivery wakeup wait failed; polling",
			slog.String("channel", w.channel.name),
			slog.Any("error", err),
		)
		w.increaseBackoff()

		return w.sleep(ctx, w.pollInterval)
	}

	if consumed {
		w.observe("consumed")
		w.Reset()
	} else {
		w.observe("timeout")
		w.increaseBackoff()
	}

	return ctx.Err() == nil
}

// Reset은 처리한 행이 있었을 때 부른다. 뒤따르는 행이 guard에 억제됐어도 다음 대기를 짧게 시작해 바로 잡는다.
func (w *Waiter) Reset() {
	w.currentWait = w.backoffMin
}

func (w *Waiter) waitForSignal(ctx context.Context, timeout time.Duration) (bool, error) {
	cmd := w.cache.B().Brpop().Key(w.channel.queue).Timeout(timeout.Seconds()).Build()

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
	w.currentWait = backoff.NextExponentialBackoff(w.currentWait, w.pollInterval, w.backoffMin)
}

func (w *Waiter) observe(result string) {
	waitResults().WithLabelValues(w.channel.name, result).Inc()
}
