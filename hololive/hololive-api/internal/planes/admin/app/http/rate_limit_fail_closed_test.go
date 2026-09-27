package apphttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

// DEC-20260926-hololive-source-fallbacks-retirement: /api/holo rate limit 판정 실패는 요청을 통과시키지 않는다.
func TestAPIRateLimitHandlerFailsClosedOnCheckError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limiter, err := ratelimit.NewSlidingWindowLimiter(unusedLowLevelCache{}, "test:holo:ip", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSlidingWindowLimiter() error = %v", err)
	}

	handler := apiRateLimitHandler{limiter: limiter, limit: 0, window: time.Minute, logger: slog.New(slog.DiscardHandler)}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
	c.Request.RemoteAddr = "203.0.113.7:1234"

	handler.Handle(c)

	if !c.IsAborted() {
		t.Fatal("rate limit check failure must abort the request")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
