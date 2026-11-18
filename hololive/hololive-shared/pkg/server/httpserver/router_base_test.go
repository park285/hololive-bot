package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestApplyBaseMiddlewareSetsSecurityAndRequestIDHeaders(t *testing.T) {
	prevMode := gin.Mode()

	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	applyBaseMiddleware(t.Context(), router, slog.New(slog.DiscardHandler), "/health", "/ready")

	for _, path := range []string{"/health", "/ready"} {
		router.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}

	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rr.Code, http.StatusOK)
		}

		if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s missing X-Content-Type-Options", path)
		}

		if rr.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s missing X-Request-ID", path)
		}
	}
}
