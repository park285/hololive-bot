package apiclient

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// sharedState는 같은 Holodex API key를 쓰는 hololive-api와 alarm-worker가 429 대기와 circuit open을 함께 지키게 한다.
// Valkey 오류는 로컬 상태만으로 진행하고(fail-open) 경고 로그와 counter로 드러낸다. 공유 상태는 만료·축출돼도
// 되는 rate limit 상태이며, 같은 Valkey를 쓰는 분산 rate limit은 계속 fail-closed다.
type sharedState interface {
	Extend(ctx context.Context, key string, until time.Time) error
	OpenOnce(ctx context.Context, key string, until time.Time) (bool, error)
	Deadline(ctx context.Context, key string) (time.Time, error)
}

const (
	// 공유 기록은 호출자 취소와 분리하되 요청 흐름을 오래 붙잡지 않게 짧게 끝낸다.
	sharedStateWriteTimeout = time.Second

	sharedStateCooldownRead  = "cooldown_read"
	sharedStateCooldownWrite = "cooldown_write"
	sharedStateCircuitRead   = "circuit_read"
	sharedStateCircuitWrite  = "circuit_write"
)

var sharedStateFailures = sync.OnceValue(func() *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_holodex_shared_state_failures_total",
		Help: "Holodex shared cooldown and circuit state operations that failed and fell back to process-local state.",
	}, []string{"operation"})
})

func (c *APIClient) sharedCooldownKey() string {
	return c.distributedRLCfg.BucketBase + ":cooldown"
}

func (c *APIClient) sharedCircuitKey() string {
	return c.distributedRLCfg.BucketBase + ":circuit_open"
}

// publishCooldown은 받은 429 대기 시각을 다른 프로세스에 알린다. 호출자가 취소해도 대기 시각을 지우지 않는 계약을 따른다.
func (c *APIClient) publishCooldown(ctx context.Context, until time.Time) {
	if c.shared == nil {
		return
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedStateWriteTimeout)
	defer cancel()

	if err := c.shared.Extend(writeCtx, c.sharedCooldownKey(), until); err != nil {
		c.recordSharedStateFailure(sharedStateCooldownWrite, err)
	}
}

// mergeSharedCooldown은 다른 프로세스가 받은 더 늦은 429 대기 시각을 로컬 cooldown에 합친다.
func (c *APIClient) mergeSharedCooldown(ctx context.Context) {
	if c.shared == nil {
		return
	}

	until, err := c.shared.Deadline(ctx, c.sharedCooldownKey())
	if err != nil {
		c.recordSharedStateReadFailure(ctx, sharedStateCooldownRead, err)

		return
	}

	c.extendLocalCooldown(until)
}

// sharedCircuitRemaining은 다른 프로세스가 연 circuit의 남은 시간을 돌려준다. 열려 있지 않거나 읽지 못하면 0이다.
func (c *APIClient) sharedCircuitRemaining(ctx context.Context) time.Duration {
	if c.shared == nil {
		return 0
	}

	until, err := c.shared.Deadline(ctx, c.sharedCircuitKey())
	if err != nil {
		c.recordSharedStateReadFailure(ctx, sharedStateCircuitRead, err)

		return 0
	}

	return time.Until(until)
}

// publishCircuitOpen은 로컬 circuit이 열린 시점에 한 번만 공유 open을 연다. 이미 열려 있으면 만료 시각을 늘리지 않아
// circuitResetTimeout보다 긴 재시도 지연(retry scheduler) 불변식을 지킨다.
func (c *APIClient) publishCircuitOpen(ctx context.Context) bool {
	if c.shared == nil {
		return false
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedStateWriteTimeout)
	defer cancel()

	opened, err := c.shared.OpenOnce(writeCtx, c.sharedCircuitKey(), time.Now().Add(circuitResetTimeout))
	if err != nil {
		c.recordSharedStateFailure(sharedStateCircuitWrite, err)

		return false
	}

	return opened
}

// recordSharedStateReadFailure는 호출자 ctx로 읽다 실패한 경우를 기록한다. 호출자 취소·기한 만료는 Valkey 장애가
// 아니므로 counter와 Warn에 넣지 않고, 요청은 뒤따르는 ctx 검사에서 끝난다.
func (c *APIClient) recordSharedStateReadFailure(ctx context.Context, operation string, err error) {
	if ctx.Err() != nil {
		return
	}

	c.recordSharedStateFailure(operation, err)
}

func (c *APIClient) recordSharedStateFailure(operation string, err error) {
	sharedStateFailures().WithLabelValues(operation).Inc()
	c.logger.Warn("Holodex shared state unavailable; using process-local state",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
}
