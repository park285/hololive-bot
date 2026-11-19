package wakeup

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type Channel struct {
	name  string
	queue string
	guard string
}

func (c Channel) Name() string {
	return c.name
}

func (c Channel) Queue() string {
	return c.queue
}

var YouTubeDelivery = Channel{name: "youtube_delivery", queue: "youtube:delivery:wakeup", guard: "youtube:delivery:wakeup:guard"}

const (
	guardTTL      = 3 * time.Second
	queueTTL      = 5 * time.Second
	signalTimeout = 500 * time.Millisecond
)

var signalResults = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_delivery_wakeup_signals_total",
		Help: "Delivery wakeup signals by channel and result (sent, suppressed, failed, expire_failed).",
	}, []string{"channel", "result"})
})

type Signaler struct {
	cache   cache.LowLevelCache
	channel Channel
	logger  *slog.Logger
}

func NewSignaler(c cache.LowLevelCache, channel Channel, logger *slog.Logger) *Signaler {
	if reflectutil.IsNil(c) {
		return nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Signaler{cache: c, channel: channel, logger: logger}
}

func (s *Signaler) Signal(ctx context.Context) {
	if s == nil {
		return
	}

	signalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signalTimeout)
	defer cancel()

	client := s.cache.GetClient()

	guard := s.cache.B().Set().Key(s.channel.guard).Value("1").Nx().ExSeconds(int64(guardTTL / time.Second)).Build()
	if err := client.Do(signalCtx, guard).Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			s.observe("suppressed")

			return
		}

		s.fail("failed", fmt.Errorf("acquire guard: %w", err))

		return
	}

	results := client.DoMulti(signalCtx,
		s.cache.B().Lpush().Key(s.channel.queue).Element("1").Build(),
		s.cache.B().Expire().Key(s.channel.queue).Seconds(int64(queueTTL/time.Second)).Build(),
	)
	if len(results) != 2 {
		s.fail("failed", fmt.Errorf("unexpected result count: %d", len(results)))

		return
	}

	if err := results[0].Error(); err != nil {
		s.fail("failed", fmt.Errorf("push: %w", err))

		return
	}

	s.observe("sent")

	if err := results[1].Error(); err != nil {
		s.fail("expire_failed", fmt.Errorf("expire: %w", err))
	}
}

func (s *Signaler) observe(result string) {
	signalResults().WithLabelValues(s.channel.name, result).Inc()
}

func (s *Signaler) fail(result string, err error) {
	s.observe(result)
	s.logger.Warn("Delivery wakeup signal failed; consumer polling will pick up the committed rows",
		slog.String("channel", s.channel.name),
		slog.String("result", result),
		slog.Any("error", err),
	)
}
