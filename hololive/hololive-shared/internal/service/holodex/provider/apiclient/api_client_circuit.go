package apiclient

import (
	"context"
	"log/slog"
	"time"
)

func (c *APIClient) rejectIfCircuitOpen(ctx context.Context) error {
	if !c.breaker.Allow() {
		return c.circuitOpenError(c.breaker.RetryAfter(), false)
	}

	return c.rejectIfSharedCircuitOpen(ctx)
}

func (c *APIClient) rejectIfSharedCircuitOpen(ctx context.Context) error {
	remaining := c.sharedCircuitRemaining(ctx)
	if remaining <= 0 {
		return nil
	}

	return c.circuitOpenError(remaining, true)
}

func (c *APIClient) circuitOpenError(remaining time.Duration, shared bool) error {
	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remaining.Milliseconds()), slog.Bool("shared", shared))

	return NewAPIError("Circuit breaker open", 503)
}

func (c *APIClient) IsCircuitOpen() bool {
	return c.breaker.IsOpen()
}

func (c *APIClient) openCircuit(ctx context.Context) {
	if opened := c.breaker.RecordFailure(); opened {
		c.logger.Error("Holodex circuit breaker opened",
			slog.Int("failure_count", int(c.breaker.Failures())),
			slog.Duration("reset_timeout", circuitResetTimeout),
			slog.Bool("shared_opened", c.publishCircuitOpen(ctx)),
		)
	}
}

func (c *APIClient) resetCircuit() {
	c.breaker.RecordSuccess()
}
