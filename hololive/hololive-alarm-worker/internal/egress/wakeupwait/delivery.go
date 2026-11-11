package wakeupwait

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/wakeup"
)

// 깨어난 직후 첫 재조회 간격이다. 이 값은 alarm dispatch 프로필의 idle_backoff_min_ms와 같고, guard(3s)가 억제한 행을
// 250ms·500ms·1s·2s 재조회가 guard 창 안에 잡는다.
const deliveryInitialBackoff = 250 * time.Millisecond

var deliveryWaitResults = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_delivery_wakeup_waits_total",
		Help: "Delivery consumer idle waits by channel and result (consumed, timeout, error).",
	}, []string{"channel", "result"})
})

// NewDelivery는 hololive-api가 신호를 보내는 delivery channel의 Waiter다. 인자 pollInterval은 consumer 프로필의
// poll_interval_ms이고 BRPOP 상한과 Valkey 오류·cache 미설정 때 대기로 함께 쓴다.
func NewDelivery(c cache.LowLevelCache, channel wakeup.Channel, pollInterval time.Duration, logger *slog.Logger) (*Waiter, error) {
	waiter, err := New(c, Config{
		Queue:        channel.Queue(),
		PollInterval: pollInterval,
		BackoffMin:   min(deliveryInitialBackoff, pollInterval),
		BackoffMax:   pollInterval,
	}, deliveryObserver{channel: channel.Name()}, logger)
	if err != nil {
		return nil, fmt.Errorf("new %s wakeup waiter: %w", channel.Name(), err)
	}

	return waiter, nil
}

// deliveryObserver는 BRPOP 결과만 hololive_delivery_wakeup_waits_total{channel,result}로 센다.
type deliveryObserver struct {
	channel string
}

func (o deliveryObserver) SignalWait(result Result, _ time.Duration) {
	deliveryWaitResults().WithLabelValues(o.channel, string(result)).Inc()
}

func (deliveryObserver) PollSleep(bool, time.Duration) {}
