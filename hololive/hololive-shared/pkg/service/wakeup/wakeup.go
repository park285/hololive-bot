// Package wakeup은 PostgreSQL outbox commit 뒤 다른 프로세스의 consumer를 깨우는 payload 없는 Valkey 신호다.
// 신호는 정본이 아니다. 유실·중복·축출돼도 consumer가 poll 주기 안에 PostgreSQL에서 다시 claim하므로 producer는
// 신호 실패를 적재 실패로 바꾸지 않는다(fail-open). 이 패키지는 신호 key 계약과 producer만 두고, consumer 대기는
// alarm-worker internal/egress/wakeupwait가 구현한다.
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

// Channel은 producer와 consumer가 함께 쓰는 신호 key 쌍이다. 신호 key 이름은 hololive-api와 alarm-worker 사이 계약이다.
type Channel struct {
	name  string
	queue string
	guard string
}

// Name은 channel을 구분하는 지표 label 값이다.
func (c Channel) Name() string {
	return c.name
}

// Queue는 consumer가 BRPOP으로 기다리는 신호 list key다.
func (c Channel) Queue() string {
	return c.queue
}

// YouTubeDelivery는 hololive-api YouTube plane이 youtube_notification_outbox 행을 commit한 뒤 alarm-worker를 깨운다.
var YouTubeDelivery = Channel{name: "youtube_delivery", queue: "youtube:delivery:wakeup", guard: "youtube:delivery:wakeup:guard"}

const (
	// 신호 guard는 짧은 시간 안의 반복 신호를 하나로 줄인다. 억제된 행은 consumer가 깨어난 뒤의 짧은 재조회가 잡는다.
	guardTTL = 3 * time.Second
	// 대기하는 consumer가 없을 때 신호 list가 남지 않게 한다.
	queueTTL = 5 * time.Second
	// 신호는 commit 뒤 부가 작업이므로 Valkey 지연이 producer 루프를 오래 붙잡지 않게 한다.
	signalTimeout = 500 * time.Millisecond
)

var signalResults = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_delivery_wakeup_signals_total",
		Help: "Delivery wakeup signals by channel and result (sent, suppressed, failed, expire_failed).",
	}, []string{"channel", "result"})
})

// Signaler는 PostgreSQL commit 뒤 consumer에게 확인할 행이 생겼다고 알린다.
type Signaler struct {
	cache   cache.LowLevelCache
	channel Channel
	logger  *slog.Logger
}

// NewSignaler는 cache가 없으면 nil을 돌려준다. 값이 nil인 Signaler의 Signal은 아무것도 하지 않아 consumer polling만 남는다.
func NewSignaler(c cache.LowLevelCache, channel Channel, logger *slog.Logger) *Signaler {
	if reflectutil.IsNil(c) {
		return nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Signaler{cache: c, channel: channel, logger: logger}
}

// Signal은 PostgreSQL commit이 끝난 뒤에만 부른다. 호출자 취소와 분리해 짧은 상한 안에서 SET NX EX guard,
// LPUSH, EXPIRE를 보내며 실패는 metric과 Warn으로만 드러낸다.
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
