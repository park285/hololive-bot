package apiclient

import (
	"context"
	"log/slog"
	"time"
)

// rejectIfCircuitOpen은 Allow() 기반으로 동작합니다.
// 지정한 timeout이 경과하면 reset side-effect가 발생하므로 자동 reset 후
// failures=0부터 재카운트가 시작됩니다. 로컬 circuit이 닫혀 있어도 다른 프로세스가 연 공유 circuit이면 거절합니다.
func (c *APIClient) rejectIfCircuitOpen(ctx context.Context) error {
	if !c.breaker.Allow() {
		return c.circuitOpenError(c.breaker.RetryAfter(), false)
	}

	return c.rejectIfSharedCircuitOpen(ctx)
}

// rejectIfSharedCircuitOpen은 다른 프로세스가 연 공유 circuit만 확인합니다. 로컬 breaker의 Allow() reset 부수 효과가
// 없으므로, 진입 검사를 통과한 요청이 재시도하기 전에도 다시 불러 open 창에 HTTP 요청을 보내지 않게 합니다.
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

// IsCircuitOpen은 read-only 상태 조회입니다. 부수 효과는 없습니다.
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
