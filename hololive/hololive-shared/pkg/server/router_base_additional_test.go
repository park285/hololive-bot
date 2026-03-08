package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestApplyBaseMiddleware_PreservesIncomingRequestID(t *testing.T) {
	t.Parallel()

	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	ApplyBaseMiddleware(router, context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), BaseMiddlewareOptions{})
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "worker-2-request-id")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := rr.Header().Get("X-Request-ID"), "worker-2-request-id"; got != want {
		t.Fatalf("X-Request-ID = %q, want %q", got, want)
	}
}

func TestApplyBaseMiddlewareAndRegisterHealthRoutes_NilInputsNoPanic(t *testing.T) {
	t.Parallel()

	ApplyBaseMiddleware(nil, context.Background(), nil, BaseMiddlewareOptions{})
	RegisterHealthRoutes(nil)
}
