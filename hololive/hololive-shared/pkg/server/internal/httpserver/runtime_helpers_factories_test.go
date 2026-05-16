package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
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
	metricsReq.Header.Set(middleware.APIKeyHeader, "test-key")
	metricsRes := httptest.NewRecorder()
	router.ServeHTTP(metricsRes, metricsReq)
	if metricsRes.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", metricsRes.Code, http.StatusOK)
	}
}

func TestNewTriggerRuntimeRouter(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("registers trigger routes", func(t *testing.T) {
		t.Parallel()

		triggerHandler := NewTriggerHandler(nil, nil, nil, logger)
		router, err := NewTriggerRuntimeRouter(t.Context(), logger, triggerHandler, "api-key")
		if err != nil {
			t.Fatalf("NewTriggerRuntimeRouter() error = %v", err)
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, triggercontracts.MajorEventWeeklyPath, http.NoBody)
		req.Header.Set(middleware.APIKeyHeader, "api-key")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusServiceUnavailable {
			t.Fatalf("trigger status = %d, want %d", res.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("fails closed when api key missing", func(t *testing.T) {
		t.Parallel()

		triggerHandler := NewTriggerHandler(nil, nil, nil, logger)
		router, err := NewTriggerRuntimeRouter(t.Context(), logger, triggerHandler, "")
		if err == nil {
			t.Fatal("NewTriggerRuntimeRouter() error = nil, want non-nil")
		}
		if router != nil {
			t.Fatal("NewTriggerRuntimeRouter() router = non-nil, want nil")
		}
		if err.Error() != "API_SECRET_KEY required" {
			t.Fatalf("NewTriggerRuntimeRouter() error = %q, want %q", err.Error(), "API_SECRET_KEY required")
		}
	})
}
