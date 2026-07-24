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

package apphttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/valkey-io/valkey-go"
)

type unusedLowLevelCache struct{}

func (unusedLowLevelCache) GetClient() valkey.Client { return nil }

func (unusedLowLevelCache) DoMulti(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
	return nil
}

func (unusedLowLevelCache) Builder() valkey.Builder { return valkey.Builder{} }

func (unusedLowLevelCache) B() valkey.Builder { return valkey.Builder{} }

func TestAPIRateLimitMiddlewareFailOpenNoCacheCountsWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	before := testutil.ToFloat64(apiRateLimitFailOpenTotal.WithLabelValues(rateLimitFailOpenReasonNoCache))

	logger := slog.New(slog.DiscardHandler)
	first := apiRateLimitMiddleware(nil, logger)
	second := apiRateLimitMiddleware(nil, logger)

	if first == nil || second == nil {
		t.Fatal("apiRateLimitMiddleware returned nil handler")
	}

	if got := testutil.ToFloat64(apiRateLimitFailOpenTotal.WithLabelValues(rateLimitFailOpenReasonNoCache)); got-before != 2 {
		t.Fatalf("no_cache fail-open delta = %v, want 2", got-before)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
	first(c)
	if c.IsAborted() {
		t.Fatal("no-cache fail-open middleware must not abort the request")
	}
}

func TestAPIRateLimitHandlerFailOpenOnCheckError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limiter, err := ratelimit.NewSlidingWindowLimiter(unusedLowLevelCache{}, "test:holo:ip", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSlidingWindowLimiter() error = %v", err)
	}

	handler := apiRateLimitHandler{
		limiter: limiter,
		limit:   0,
		window:  time.Minute,
		logger:  slog.New(slog.DiscardHandler),
	}

	before := testutil.ToFloat64(apiRateLimitFailOpenTotal.WithLabelValues(rateLimitFailOpenReasonCheckFailed))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
	c.Request.RemoteAddr = "203.0.113.7:1234"

	handler.Handle(c)

	if got := testutil.ToFloat64(apiRateLimitFailOpenTotal.WithLabelValues(rateLimitFailOpenReasonCheckFailed)); got-before != 1 {
		t.Fatalf("check_failed fail-open delta = %v, want 1", got-before)
	}
	if c.IsAborted() {
		t.Fatal("check-failed fail-open must not abort the request")
	}
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("check-failed fail-open must not return 429, got %d", rec.Code)
	}
}
