package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	ApplyBaseMiddleware(router, context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), BaseMiddlewareOptions{})
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ping", http.NoBody)
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

func TestApplyBaseMiddleware_RecoversHandlerPanic(t *testing.T) {
	t.Parallel()

	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	ApplyBaseMiddleware(router, context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), BaseMiddlewareOptions{})
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if body := rr.Body.String(); !strings.Contains(body, "internal_error") {
		t.Fatalf("body = %q, want internal_error payload", body)
	}
}

func TestApplyBaseMiddleware_RecoversPanicOutsideLogger(t *testing.T) {
	t.Parallel()

	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	logger := slog.New(slog.NewTextHandler(panicOnRequestLogWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ApplyBaseMiddleware(router, context.Background(), logger, BaseMiddlewareOptions{})
	router.GET("/panic-outside-logger", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic-outside-logger", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if body := rr.Body.String(); !strings.Contains(body, "internal_error") {
		t.Fatalf("body = %q, want internal_error payload", body)
	}
}

type panicOnRequestLogWriter struct{}

func (panicOnRequestLogWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("http.request.completed")) {
		panic("logger post-next panic")
	}
	return len(p), nil
}
