package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func TestNewRuntimeRouter_InternalReadyRequiresAPIKey(t *testing.T) {
	t.Parallel()

	probeCalls := 0
	router, err := NewRuntimeRouter(t.Context(), slog.New(slog.DiscardHandler), &RuntimeRouterOptions{
		APIKey: "probe-key",
		InternalReadyResponder: func(c *gin.Context) {
			probeCalls++
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeRouter() error = %v", err)
	}

	publicReady := serveRuntimeReadyRequest(t, router, "/ready", "")
	if publicReady.Code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", publicReady.Code, http.StatusOK)
	}
	if probeCalls != 0 {
		t.Fatalf("/ready invoked internal probe %d time(s), want 0", probeCalls)
	}

	noAuth := serveRuntimeReadyRequest(t, router, "/internal/ready", "")
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("/internal/ready without key status = %d, want %d", noAuth.Code, http.StatusUnauthorized)
	}
	if probeCalls != 0 {
		t.Fatalf("unauthorized /internal/ready invoked probe %d time(s), want 0", probeCalls)
	}

	withAuth := serveRuntimeReadyRequest(t, router, "/internal/ready", "probe-key")
	if withAuth.Code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready with key status = %d, want %d", withAuth.Code, http.StatusServiceUnavailable)
	}
	if probeCalls != 1 {
		t.Fatalf("authorized /internal/ready invoked probe %d time(s), want 1", probeCalls)
	}
}

func serveRuntimeReadyRequest(t *testing.T, router http.Handler, path, apiKey string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody)
	if apiKey != "" {
		req.Header.Set(middleware.APIKeyHeader, apiKey)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
