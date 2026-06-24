// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scraping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/internal/retry"
)

// fetchPage: YouTube 페이지 HTML 가져오기 (5xx 에러 시 재시도 포함)
func (c *Client) fetchPage(ctx context.Context, pageURL string, policy ...FetchPolicy) (string, error) {
	// transient cooldown은 worker를 점유한 채 sleep하지 않고 스케줄러에 재시도 지연을 위임한다.
	if wait := c.backoffState.TransientCooldownRemaining(); wait > 0 {
		return "", &CooldownError{
			Kind:  "youtube transient",
			Delay: wait,
			Err:   ErrTransientCooldown,
		}
	}

	resolvedPolicy := resolveFetchPolicy(policy...)

	var result string

	err := retry.WithRetry(ctx, c.fetchPageRetryOptions(pageURL, resolvedPolicy), func(ctx context.Context) error {
		if err := c.fetchPagePreflight(ctx, pageURL, resolvedPolicy); err != nil {
			return err
		}

		attemptCtx, cancel := contextWithFetchAttemptTimeout(ctx, resolvedPolicy)
		defer cancel()

		var err error
		result, err = c.fetchPageOnce(attemptCtx, pageURL)
		if err != nil && errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("%w: %w", errFetchAttemptTimeout, err)
		}
		return err
	})

	if err != nil {
		c.recordFetchPageTransientError(ctx, err)
		return "", fmt.Errorf("fetchPage failed after retries: %w", err)
	}
	return result, nil
}

func resolveFetchPolicy(policy ...FetchPolicy) FetchPolicy {
	resolved := DefaultFetchPolicy
	if len(policy) == 0 {
		return resolved
	}

	override := policy[0]
	if override.MaxAttempts > 0 {
		resolved.MaxAttempts = override.MaxAttempts
	}
	if override.PerAttemptTimeout > 0 {
		resolved.PerAttemptTimeout = override.PerAttemptTimeout
	}
	if override.BaseDelay > 0 {
		resolved.BaseDelay = override.BaseDelay
	}
	if override.Jitter > 0 {
		resolved.Jitter = override.Jitter
	}
	if override.MaxDelay > 0 {
		resolved.MaxDelay = override.MaxDelay
	}
	resolved.AdmissionBlocking = override.AdmissionBlocking
	return resolved
}

func contextWithFetchAttemptTimeout(ctx context.Context, policy FetchPolicy) (context.Context, context.CancelFunc) {
	if policy.PerAttemptTimeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= policy.PerAttemptTimeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, policy.PerAttemptTimeout)
}

func (c *Client) fetchPageRetryOptions(pageURL string, policy FetchPolicy) retry.RetryOptions {
	return retry.RetryOptions{
		MaxAttempts:   policy.MaxAttempts,
		BaseDelay:     policy.BaseDelay,
		Jitter:        policy.Jitter,
		MaxDelay:      policy.MaxDelay,
		DelayOverride: fetchPageRetryDelayOverride,
		ShouldRetry:   shouldRetryFetchPage,
		OnRetry: func(attempt int, err error, delay time.Duration) {
			if isRetryableTransportError(err) {
				c.closeIdleConnections()
			}
			slog.Debug("Scraper retry",
				"url", pageURL,
				"attempt", attempt,
				"delay", delay.Round(time.Millisecond),
				"error", err)
		},
	}
}

func fetchPageRetryDelayOverride(err error, computed time.Duration) (time.Duration, bool) {
	if retryAfter := extractHTTPRetryAfter(err); retryAfter > 0 {
		return retryAfter, true
	}

	var cooldown *CooldownError
	if errors.As(err, &cooldown) && cooldown.RetryDelay() > 0 {
		return cooldown.RetryDelay(), true
	}

	return computed, false
}

func shouldRetryFetchPage(err error) bool {
	if errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrBlockedResponse) ||
		errors.Is(err, ErrResponseTooLarge) {
		return false
	}
	return isRetryableFetchPageError(err)
}

func (c *Client) recordFetchPageTransientError(ctx context.Context, err error) {
	if ctx.Err() != nil || IsAdmissionDeferred(err) {
		return
	}
	if isRetryableFetchPageError(err) {
		c.backoffState.RecordTransientErrorWithSuggestedCooldown(extractHTTPRetryAfter(err))
	}
}
