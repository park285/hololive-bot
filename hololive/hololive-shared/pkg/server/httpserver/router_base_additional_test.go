package httpserver

import (
	"bytes"
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
	ApplyBaseMiddleware(t.Context(), router, slog.New(slog.DiscardHandler), BaseMiddlewareOptions{})
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ping", http.NoBody)
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

	ApplyBaseMiddleware(t.Context(), nil, nil, BaseMiddlewareOptions{})
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
	ApplyBaseMiddleware(t.Context(), router, slog.New(slog.DiscardHandler), BaseMiddlewareOptions{})
	router.GET("/panic", func(_ *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", http.NoBody)
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
	ApplyBaseMiddleware(t.Context(), router, logger, BaseMiddlewareOptions{})
	router.GET("/panic-outside-logger", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic-outside-logger", http.NoBody)
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
