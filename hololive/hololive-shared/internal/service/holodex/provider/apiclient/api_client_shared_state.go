package apiclient

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type sharedState interface {
	Extend(ctx context.Context, key string, until time.Time) error
	OpenOnce(ctx context.Context, key string, until time.Time) (bool, error)
	Deadline(ctx context.Context, key string) (time.Time, error)
}

const (
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
