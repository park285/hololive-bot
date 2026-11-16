package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

func TestNewHealthOnlyRuntimeRouter(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	router, err := NewHealthOnlyRuntimeRouter(t.Context(), logger, "test-key")
	if err != nil {
		t.Fatalf("NewHealthOnlyRuntimeRouter() error = %v", err)
	}

	healthReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	healthRes := httptest.NewRecorder()
	router.ServeHTTP(healthRes, healthReq)

	if healthRes.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", healthRes.Code, http.StatusOK)
	}

	metricsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsReq.Header.Set(httputil.HeaderAPIKey, "test-key")

	metricsRes := httptest.NewRecorder()
	router.ServeHTTP(metricsRes, metricsReq)

	if metricsRes.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", metricsRes.Code, http.StatusOK)
	}
}
