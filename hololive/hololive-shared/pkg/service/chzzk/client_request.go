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

package chzzk

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/shared-go/pkg/jsonutil"

	apperrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

// IsCircuitOpen은 read-only 상태 조회입니다. side-effect가 없습니다.
func (c *Client) IsCircuitOpen() bool {
	return c.breaker.IsOpen()
}

func (c *Client) newRequest(ctx context.Context, method, targetURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "api.capu.blog/hololive-bot (Chzzk API client)")

	return req, nil
}

func (c *Client) executeRequest(
	op string,
	req *http.Request,
	readErrorPrefix string,
	handleBody func([]byte) error,
) error {
	body, err := c.doRequest(op, req, readErrorPrefix)
	if err != nil {
		return err
	}

	if err := handleBody(body); err != nil {
		c.recordFailure()
		return err
	}

	c.breaker.RecordSuccess()

	return nil
}

func (c *Client) doRequest(op string, req *http.Request, readErrorPrefix string) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordFailure()

		return nil, &apperrors.APIError{
			Operation:  op,
			StatusCode: 0,
			Err:        err,
		}
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.handleStatusCodeError(resp.StatusCode)

		return nil, &apperrors.APIError{
			Operation:  op,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, c.maxResponseBodyBytes)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("%s: %w", readErrorPrefix, err)
	}

	return body, nil
}

// rejectIfCircuitOpen은 Allow() 기반으로 동작합니다.
// timeout 경과 시 reset(open=false, failures=0) side-effect가 발생하므로
// 자동 reset 후 첫 요청이 통과하고, failures=0부터 재카운트가 시작됩니다.
func (c *Client) rejectIfCircuitOpen() error {
	if c.breaker.Allow() {
		return nil
	}

	remainingMs := c.breaker.RetryAfter().Milliseconds()

	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remainingMs))

	return apperrors.CircuitOpenError{RetryAfterMs: remainingMs}
}

func (c *Client) handleStatusCodeError(statusCode int) {
	if statusCode >= 500 || statusCode == http.StatusTooManyRequests {
		c.recordFailure()
		c.logger.Warn("Server error or rate limit",
			slog.Int("status", statusCode),
			slog.Int("failure_count", int(c.breaker.Failures())),
		)
	}
}

func (c *Client) recordFailure() {
	if opened := c.breaker.RecordFailure(); opened {
		c.logger.Error("Chzzk circuit breaker opened",
			slog.Duration("reset_timeout", constants.CircuitBreakerConfig.ResetTimeout),
		)
	}
}
