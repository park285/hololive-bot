package holodex

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/kapu/hololive-shared/internal/retry"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

func waitDistributedRateLimitDecision(ctx context.Context, bucket string, decision ratelimit.Decision) (bool, error) {
	if decision.Allowed {
		return true, nil
	}
	if decision.RetryAfter <= 0 {
		return false, fmt.Errorf(
			"distributed rate limiter denied without retry_after: bucket=%s current=%d limit=%d",
			bucket,
			decision.Current,
			decision.Limit,
		)
	}
	if !ctxutil.SleepWithContext(ctx, decision.RetryAfter) {
		return false, fmt.Errorf("distributed rate limiter wait canceled: %w", ctx.Err())
	}
	return false, nil
}

func distributedRateLimitBucket(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		trimmed = "root"
	}
	normalized := strings.ReplaceAll(trimmed, "/", ":")
	return constants.HolodexDistributedRateLimitConfig.BucketBase + ":" + normalized
}

func (c *APIClient) waitBackoff(ctx context.Context, attempt int) error {
	delay := retry.ComputeBackoffDelay(attempt, constants.RetryConfig.BaseDelay, constants.RetryConfig.Jitter)
	if !ctxutil.SleepWithContext(ctx, delay) {
		return fmt.Errorf("context canceled during backoff: %w", ctx.Err())
	}
	return nil
}
