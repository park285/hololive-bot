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
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/retry"
)

func (c *Client) ResolveCommunityPostPublishedAt(ctx context.Context, postID string) (*time.Time, error) {
	trimmedPostID := strings.TrimSpace(postID)
	if trimmedPostID == "" {
		return nil, ErrCommunityPublishedAtNotFound
	}

	postURL := "https://www.youtube.com/post/" + url.PathEscape(trimmedPostID)

	html, err := c.fetchPage(ctx, postURL, MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch community post page: %w", err)
	}

	publishedAt, err := extractCommunityPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract community published_at: %w", err)
	}

	return publishedAt, nil
}

func (c *Client) ResolveVideoPublishedAt(ctx context.Context, videoID string) (*time.Time, error) {
	trimmedVideoID := strings.TrimSpace(videoID)
	if trimmedVideoID == "" {
		return nil, ErrPublishedAtNotFound
	}

	params := url.Values{}
	params.Set("v", trimmedVideoID)
	watchURL := "https://www.youtube.com/watch?" + params.Encode()

	html, err := c.fetchPage(ctx, watchURL, MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch video page: %w", err)
	}

	publishedAt, err := extractPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract video published_at: %w", err)
	}

	return publishedAt, nil
}

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

	resolvedPolicy := DefaultFetchPolicy
	if len(policy) > 0 && policy[0].MaxAttempts > 0 {
		resolvedPolicy = policy[0]
	}

	var result string

	err := retry.WithRetry(ctx, c.fetchPageRetryOptions(pageURL, resolvedPolicy), func(ctx context.Context) error {
		var err error
		result, err = c.fetchPageOnce(ctx, pageURL)
		return err
	})

	if err != nil {
		c.recordFetchPageTransientError(ctx, err)
		return "", fmt.Errorf("fetchPage failed after retries: %w", err)
	}
	return result, nil
}

func (c *Client) fetchPageRetryOptions(pageURL string, policy FetchPolicy) retry.RetryOptions {
	return retry.RetryOptions{
		MaxAttempts: policy.MaxAttempts,
		BaseDelay:   2 * time.Second,
		Jitter:      1500 * time.Millisecond,
		ShouldRetry: shouldRetryFetchPage,
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

func shouldRetryFetchPage(err error) bool {
	if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrForbidden) {
		return false
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return isRetryableStatusCode(statusErr.code)
	}
	return isRetryableTransportError(err)
}

func (c *Client) recordFetchPageTransientError(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	if isRetryableStatusError(err) || isRetryableTransportError(err) {
		c.backoffState.RecordTransientErrorWithSuggestedCooldown(extractHTTPRetryAfter(err))
	}
}
