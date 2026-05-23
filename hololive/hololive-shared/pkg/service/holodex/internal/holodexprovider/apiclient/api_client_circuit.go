package apiclient

import (
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func (c *APIClient) rejectIfCircuitOpen() error {
	if !c.IsCircuitOpen() {
		return nil
	}

	c.circuitMu.RLock()
	var remainingMs int64
	if c.circuitOpenUntil != nil {
		remainingMs = time.Until(*c.circuitOpenUntil).Milliseconds()
	}
	c.circuitMu.RUnlock()

	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remainingMs))
	return NewAPIError("Circuit breaker open", 503, map[string]any{
		"retry_after_ms": remainingMs,
	})
}

func (c *APIClient) IsCircuitOpen() bool {
	c.circuitMu.RLock()
	defer c.circuitMu.RUnlock()

	if c.circuitOpenUntil == nil {
		return false
	}

	if time.Now().After(*c.circuitOpenUntil) {
		return false
	}

	return true
}

func (c *APIClient) openCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	resetTime := time.Now().Add(constants.CircuitBreakerConfig.ResetTimeout)
	c.circuitOpenUntil = &resetTime
	c.failureCount = 0

	c.logger.Error("Holodex circuit breaker opened",
		slog.Duration("reset_timeout", constants.CircuitBreakerConfig.ResetTimeout),
	)
}

func (c *APIClient) resetCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount = 0
	c.circuitOpenUntil = nil
}

func (c *APIClient) incrementFailureCount() int {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount++
	return c.failureCount
}
