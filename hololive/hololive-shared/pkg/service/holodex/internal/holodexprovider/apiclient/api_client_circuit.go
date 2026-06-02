package apiclient

import (
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// rejectIfCircuitOpen은 Allow() 기반으로 동작합니다.
// timeout 경과 시 reset side-effect가 발생하므로 자동 reset 후
// failures=0부터 재카운트가 시작됩니다.
func (c *APIClient) rejectIfCircuitOpen() error {
	if c.breaker.Allow() {
		return nil
	}

	remainingMs := c.breaker.RetryAfter().Milliseconds()

	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remainingMs))
	return NewAPIError("Circuit breaker open", 503, map[string]any{
		"retry_after_ms": remainingMs,
	})
}

// IsCircuitOpen은 read-only 상태 조회입니다. side-effect가 없습니다.
func (c *APIClient) IsCircuitOpen() bool {
	return c.breaker.IsOpen()
}

// openCircuit은 실패를 기록하고, open 전이 시 Error 레벨 로그를 방출합니다.
func (c *APIClient) openCircuit() {
	if opened := c.breaker.RecordFailure(); opened {
		c.logger.Error("Holodex circuit breaker opened",
			slog.Int("failure_count", int(c.breaker.Failures())),
			slog.Duration("reset_timeout", constants.CircuitBreakerConfig.ResetTimeout),
		)
	}
}

func (c *APIClient) resetCircuit() {
	c.breaker.RecordSuccess()
}

func (c *APIClient) forceOpenedAtForTest(t time.Time) {
	c.breaker.SetOpenedAtForTest(t)
}
