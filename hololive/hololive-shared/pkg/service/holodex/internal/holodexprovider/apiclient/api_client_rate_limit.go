package apiclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/park285/shared-go/pkg/backoff"
)

func (c *APIClient) waitForRateLimiter(ctx context.Context, path string) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait failed: %w", err)
	}
	return c.waitForDistributedRateLimiter(ctx, path)
}

func (c *APIClient) waitForDistributedRateLimiter(ctx context.Context, path string) error {
	if c.distributed == nil || !c.distributedRLCfg.Enabled {
		return nil
	}

	return c.waitForDistributedRateLimitBucket(ctx, c.distributedRateLimitBucket(path))
}

func (c *APIClient) waitForDistributedRateLimitBucket(ctx context.Context, bucket string) error {
	for {
		decision, err := c.allowDistributedRateLimit(ctx, bucket)
		if err != nil {
			return err
		}
		done, err := waitDistributedRateLimitDecision(ctx, bucket, decision)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (c *APIClient) allowDistributedRateLimit(ctx context.Context, bucket string) (ratelimit.Decision, error) {
	decision, err := c.distributed.Allow(
		ctx,
		bucket,
		c.distributedRLCfg.Limit,
		c.distributedRLCfg.Window,
	)
	if err != nil {
		return ratelimit.Decision{}, fmt.Errorf("distributed rate limiter allow failed: %w", err)
	}
	return decision, nil
}

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

func (c *APIClient) distributedRateLimitBucket(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		trimmed = "root"
	}
	normalized := strings.ReplaceAll(trimmed, "/", ":")
	return c.distributedRLCfg.BucketBase + ":" + normalized
}

func (c *APIClient) waitBackoff(ctx context.Context, attempt int) error {
	delay := backoff.ComputeExponentialBackoff(attempt, constants.RetryConfig.BaseDelay, 0, constants.RetryConfig.Jitter)
	if !ctxutil.SleepWithContext(ctx, delay) {
		return fmt.Errorf("context canceled during backoff: %w", ctx.Err())
	}
	return nil
}
