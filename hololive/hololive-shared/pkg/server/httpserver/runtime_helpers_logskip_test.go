package httpserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRuntimeRouterSkipsObservabilityHeartbeatLogging(t *testing.T) {
	t.Parallel()

	for target, wantLogged := range map[string]bool{
		"/__observability/trace-heartbeat": false,
		"/__observability/":                false,
		"/__observability":                 true,
		"/no-such-route":                   true,
	} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		router, err := NewRuntimeRouter(t.Context(), logger, &RuntimeRouterOptions{})
		if err != nil {
			t.Fatalf("NewRuntimeRouter() error = %v", err)
		}

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))

		if got, want := rr.Code, http.StatusNotFound; got != want {
			t.Fatalf("GET %q status = %d, want %d", target, got, want)
		}
		if got := strings.Contains(logs.String(), "http.request.completed"); got != wantLogged {
			t.Fatalf("GET %q logged = %v, want %v", target, got, wantLogged)
		}
	}
}
