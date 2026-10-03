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

package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type unusedLowLevelCache struct{}

func (unusedLowLevelCache) GetClient() valkey.Client { return nil }

func (unusedLowLevelCache) DoMulti(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
	return nil
}

func (unusedLowLevelCache) Builder() valkey.Builder { return valkey.Builder{} }

func (unusedLowLevelCache) B() valkey.Builder { return valkey.Builder{} }

// rate limit이 켜져 있는데 cache가 없으면 통과시키는 대신 기동 오류다.
func TestAPIRateLimitMiddlewareRejectsMissingCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, err := apiRateLimitMiddleware(nil, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("apiRateLimitMiddleware(nil) error = nil, want startup error")
	}

	if handler != nil {
		t.Fatal("apiRateLimitMiddleware(nil) returned a handler")
	}
}

func TestAPIRateLimitHandlerCountsCheckFailure(t *testing.T) {
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

	before := testutil.ToFloat64(apiRateLimitCheckFailuresTotal)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
	c.Request.RemoteAddr = "203.0.113.7:1234"

	handler.Handle(c)

	if got := testutil.ToFloat64(apiRateLimitCheckFailuresTotal); got-before != 1 {
		t.Fatalf("check failure delta = %v, want 1", got-before)
	}
}
