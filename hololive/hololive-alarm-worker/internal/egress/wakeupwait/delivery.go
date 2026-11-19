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

const deliveryInitialBackoff = 250 * time.Millisecond

var deliveryWaitResults = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_delivery_wakeup_waits_total",
		Help: "Delivery consumer idle waits by channel and result (consumed, timeout, error).",
	}, []string{"channel", "result"})
})

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

type deliveryObserver struct {
	channel string
}

func (o deliveryObserver) SignalWait(result Result, _ time.Duration) {
	deliveryWaitResults().WithLabelValues(o.channel, string(result)).Inc()
}

func (deliveryObserver) PollSleep(bool, time.Duration) {}
