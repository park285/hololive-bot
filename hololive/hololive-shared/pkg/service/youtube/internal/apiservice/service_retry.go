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

package apiservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/kapu/hololive-shared/internal/retry"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func (ys *serviceImpl) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var quotaErr *QuotaExceededError
	if errors.As(err, &quotaErr) {
		return false
	}

	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return isRetryableGoogleAPIError(apiErr.Code)
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return true
}

func isRetryableGoogleAPIError(code int) bool {
	switch code {
	case 429:
		return true
	case 500, 502, 503, 504:
		return true
	case 400, 401, 403, 404:
		return false
	default:
		return true
	}
}

func (ys *serviceImpl) withRetry(ctx context.Context, fn func(context.Context) error) error {
	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: constants.RetryConfig.MaxAttempts,
		BaseDelay:   constants.RetryConfig.BaseDelay,
		Jitter:      constants.RetryConfig.Jitter,
		ShouldRetry: ys.isRetryableError,
		OnRetry: func(attempt int, err error, delay time.Duration) {
			ys.logger.Warn("YouTube API retry",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		},
	}, fn)
	if err != nil {
		return fmt.Errorf("youtube retry exhausted: %w", err)
	}
	return nil
}
