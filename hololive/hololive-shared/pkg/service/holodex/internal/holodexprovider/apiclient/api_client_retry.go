package apiclient

import (
	"context"
	stdErrors "errors"
	"log/slog"
	"net"

	"github.com/kapu/hololive-shared/pkg/constants"
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

func (c *APIClient) waitHolodexRequestBackoff(ctx context.Context, attempt int, maxAttempts int) error {
	if attempt >= maxAttempts-1 {
		return nil
	}
	return c.waitBackoff(ctx, attempt)
}

func (c *APIClient) retryAfterNetworkFailure(ctx context.Context, err error, attempt, maxAttempts int) bool {
	// 부모 ctx 취소 시 불필요한 재시도 방지
	if ctx.Err() != nil {
		return false
	}

	errorType := "network"
	if IsTimeoutError(err) {
		errorType = "timeout"
	}

	count := c.incrementFailureCount()
	if count >= constants.CircuitBreakerConfig.FailureThreshold {
		c.openCircuit()
		return false
	}

	if attempt < maxAttempts-1 {
		c.logger.Warn("Request failed, retrying",
			slog.Any("error", err),
			slog.String("error_type", errorType),
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
	var netErr net.Error
	if stdErrors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
