package apiclient

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net"
)

func (state *holodexRequestRetryState) recordAttemptError(logger *slog.Logger, path string, err error) bool {
	if err == nil {
		return false
	}

	state.lastErr = err
	if !IsTimeoutError(err) {
		return false
	}

	state.timeoutCount++
	if state.timeoutCount < state.maxTimeoutRetries {
		return false
	}

	logger.Warn("Timeout retry limit reached",
		slog.Int("timeout_count", state.timeoutCount),
		slog.String("path", path),
	)

	return true
}

func (c *APIClient) waitHolodexRequestBackoff(ctx context.Context, attempt, maxAttempts int) error {
	if attempt >= maxAttempts-1 {
		return nil
	}

	if err := c.waitBackoff(ctx, attempt); err != nil {
		return fmt.Errorf("wait backoff: %w", err)
	}

	return nil
}

func (c *APIClient) retryAfterNetworkFailure(ctx context.Context, err error, attempt, maxAttempts int) bool {
	// 부모 ctx 취소 시 불필요한 재시도 방지
	if ctx.Err() != nil {
		return false
	}

	failureKind := "network"

	if IsTimeoutError(err) {
		failureKind = "timeout"
	}

	c.openCircuit()

	if c.IsCircuitOpen() {
		return false
	}

	if attempt < maxAttempts-1 {
		// 중간 시도는 진단용이다. 재시도가 끝내 실패하면 오류를 받은 호출자가 한 번만 기록한다.
		c.logger.Debug("Request failed, retrying",
			slog.Any("error", err),
			slog.String("failure_kind", failureKind),
			slog.Int("attempt", attempt+1),
		)

		return true
	}

	return false
}

func IsTimeoutError(err error) bool {
	if stdErrors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if netErr, ok := stdErrors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}

	return false
}
