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

package scraper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const FetchPageMaxAttempts = 3

type FetchPolicy struct {
	MaxAttempts int
}

var (
	DefaultFetchPolicy              = FetchPolicy{MaxAttempts: FetchPageMaxAttempts}
	HighFrequencyChannelFetchPolicy = FetchPolicy{MaxAttempts: 1}
	MetadataResolveFetchPolicy      = FetchPolicy{MaxAttempts: 1}
)

var ErrRateLimited = errors.New("rate limited by YouTube (429)")

var ErrForbidden = errors.New("forbidden by YouTube (403)")

var ErrTransientCooldown = errors.New("youtube transient cooldown")

type CooldownError struct {
	Kind  string
	Delay time.Duration
	Err   error
}

func (e *CooldownError) Error() string {
	if e == nil {
		return "cooldown error"
	}

	return fmt.Sprintf(
		"%s cooldown remaining: %s: %v",
		e.Kind,
		e.Delay.Round(time.Second),
		e.Err,
	)
}

func (e *CooldownError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func (e *CooldownError) RetryDelay() time.Duration {
	if e == nil || e.Delay <= 0 {
		return 0
	}

	return e.Delay
}

var ErrChannelNotFound = errors.New("channel does not exist")

var ErrChannelUnavailable = errors.New("channel is unavailable")

// httpStatusError: HTTP 상태 코드 기반 에러 (재시도 판단용)
type httpStatusError struct {
	code       int
	retryAfter time.Duration
	cause      error
}

func (e *httpStatusError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("unexpected status code: %d (retry-after: %s)", e.code, e.retryAfter.Round(time.Second))
	}
	return fmt.Sprintf("unexpected status code: %d", e.code)
}

func (e *httpStatusError) Unwrap() error {
	return e.cause
}

func extractHTTPStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.code, true
}

func extractHTTPRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return 0
	}
	return statusErr.retryAfter
}

func isRetryableStatusError(err error) bool {
	statusCode, ok := extractHTTPStatusCode(err)
	return ok && isRetryableStatusCode(statusCode)
}

func isRetryableVideoPageError(err error) bool {
	return isRetryableStatusError(err) || isRetryableTransportError(err)
}

func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooEarly:
		return true
	default:
		return isRetryable5xx(code)
	}
}

// isRetryable5xx: 5xx 서버 에러인지 확인 (재시도 대상)
func isRetryable5xx(code int) bool {
	switch code {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// isRetryableTransportError: 네트워크/프록시 계층 일시 장애인지 확인
func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	// 호출자 컨텍스트 취소는 재시도하지 않는다.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// 호출자 deadline 초과는 재시도하지 않는다.
	// 단, http.Client 자체 타임아웃은 문자열 시그니처로 구분하여 재시도 허용.
	if errors.Is(err, context.DeadlineExceeded) {
		return hasTransientTransportSignature(err.Error())
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if isTimeoutOrTemporaryError(urlErr) {
			return true
		}
		if urlErr.Err == nil {
			return false
		}
		if isTimeoutOrTemporaryError(urlErr.Err) {
			return true
		}
		return hasTransientTransportSignature(urlErr.Err.Error())
	}

	if isTimeoutOrTemporaryError(err) {
		return true
	}

	return hasTransientTransportSignature(err.Error())
}

type temporaryError interface {
	Temporary() bool
}

func isTimeoutOrTemporaryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var tempErr temporaryError
	return errors.As(err, &tempErr) && tempErr.Temporary()
}

func hasTransientTransportSignature(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "connection reset by peer") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "http2: timeout awaiting response headers") ||
		strings.Contains(lower, "timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "unexpected eof")
}
