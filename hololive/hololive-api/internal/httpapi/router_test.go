package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

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
		req.Header.Set(common.APIKeyHeader, "api-key")

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

		const wantErr = "runtime router: register routes: API_SECRET_KEY required"

		if err.Error() != wantErr {
			t.Fatalf("NewTriggerRuntimeRouter() error = %q, want %q", err.Error(), wantErr)
		}
	})
}
