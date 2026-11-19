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

	ratelimitvalkey "github.com/kapu/hololive-shared/pkg/service/ratelimit/valkey"
)

type unusedLowLevelCache struct{}

func (unusedLowLevelCache) GetClient() valkey.Client { return nil }

func (unusedLowLevelCache) DoMulti(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
	return nil
}

func (unusedLowLevelCache) B() valkey.Builder { return valkey.Builder{} }

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

	limiter, err := ratelimitvalkey.NewSlidingWindowLimiter(unusedLowLevelCache{}, "test:holo:ip", slog.New(slog.DiscardHandler))
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
