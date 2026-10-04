package apiclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/backoff"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

func (c *APIClient) waitForRateLimiter(ctx context.Context, path string) error {
	// 승인 구간만 직렬화하고 HTTP 전송은 기존 세마포어의 동시성을 유지합니다.
	select {
	case c.admissionGate <- struct{}{}:
		defer func() { <-c.admissionGate }()
	case <-ctx.Done():
		return fmt.Errorf("holodex admission canceled: %w", ctx.Err())
	}

	for {
		if err := c.waitForCooldown(ctx); err != nil {
			return fmt.Errorf("wait for holodex cooldown: %w", err)
		}

		if delay := time.Until(c.nextRequestAt); delay > 0 && !retry.Sleep(ctx, delay) {
			return fmt.Errorf("holodex request interval canceled: %w", ctx.Err())
		}

		if err := c.waitForDistributedRateLimiter(ctx, path); err != nil {
			return fmt.Errorf("wait for distributed rate limiter: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("holodex admission canceled: %w", err)
		}

		if c.cooldownRemaining() <= 0 {
			// 분산 승인이 늦어져도 다음 요청은 이번 승인 완료 시각부터 간격을 지킵니다.
			c.nextRequestAt = time.Now().Add(c.requestDelay)
			return nil
		}

		// 로컬·분산 대기 중 받은 429 이후에는 이전 승인을 재사용하지 않습니다.
	}
}

func (c *APIClient) waitForDistributedRateLimiter(ctx context.Context, path string) error {
	if c.distributed == nil || !c.distributedRLCfg.Enabled {
		return nil
	}

	if err := c.waitForDistributedRateLimitBucket(ctx, c.distributedRateLimitBucket(path)); err != nil {
		return fmt.Errorf("wait for distributed rate limit bucket: %w", err)
	}

	return nil
}

func (c *APIClient) waitForDistributedRateLimitBucket(ctx context.Context, bucket string) error {
	for {
		decision, err := c.allowDistributedRateLimit(ctx, bucket)
		if err != nil {
			return fmt.Errorf("allow distributed rate limit: %w", err)
		}

		done, err := waitDistributedRateLimitDecision(ctx, bucket, decision)
		if err != nil {
			return fmt.Errorf("wait distributed rate limit decision: %w", err)
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

	if !retry.Sleep(ctx, decision.RetryAfter) {
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
	if !retry.Sleep(ctx, delay) {
		return fmt.Errorf("context canceled during backoff: %w", ctx.Err())
	}

	return nil
}
