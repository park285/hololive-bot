package readiness

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func okCheck(name string) sharedreadiness.Check {
	return sharedreadiness.Check{Name: name, Probe: func(context.Context) error { return nil }}
}

func failCheck(name string, err error) sharedreadiness.Check {
	return sharedreadiness.Check{Name: name, Probe: func(context.Context) error { return err }}
}

func TestEvaluate_AllHealthyReady(t *testing.T) {
	t.Parallel()

	code, payload := evaluate(t.Context(), sharedreadiness.NewProbe("bot", okCheck("postgres"), okCheck("valkey")))

	if code != http.StatusOK {
		t.Fatalf("evaluate status = %d, want %d", code, http.StatusOK)
	}

	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}

	deps, ok := payload["dependencies"].(map[string]bool)
	if !ok {
		t.Fatalf("dependencies type = %T, want map[string]bool", payload["dependencies"])
	}

	if !deps["postgres"] || !deps["valkey"] {
		t.Fatalf("dependencies = %v, want all available", deps)
	}
}

func TestEvaluate_DependencyDownNotReady(t *testing.T) {
	t.Parallel()

	code, payload := evaluate(t.Context(), sharedreadiness.NewProbe("admin",
		okCheck("postgres"),
		failCheck("valkey", errors.New("connection refused")),
	))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("evaluate status = %d, want %d", code, http.StatusServiceUnavailable)
	}

	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}

	deps, ok := payload["dependencies"].(map[string]bool)
	if !ok {
		t.Fatalf("dependencies type = %T, want map[string]bool", payload["dependencies"])
	}

	if deps["valkey"] {
		t.Fatal("valkey availability = true, want false")
	}

	if !deps["postgres"] {
		t.Fatal("postgres availability = false, want true")
	}
}

func TestGinHandler_HealthyReturns200(t *testing.T) {
	t.Parallel()

	code, payload := serveReady(t, GinHandler(t.Context(), sharedreadiness.NewProbe("bot", okCheck("postgres"))))

	if code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
	}

	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}

	if _, leaked := payload["workerProfile"]; leaked {
		t.Fatalf("/ready leaked worker diagnostics: %v", payload)
	}
}

func TestGinHandler_DegradedReturns503(t *testing.T) {
	t.Parallel()

	code, payload := serveReady(t, GinHandler(t.Context(), sharedreadiness.NewProbe("bot",
		okCheck("postgres"),
		failCheck("valkey", errors.New("ping failed")),
	)))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}

	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
}

func TestGinHandler_NilProbeStaticReady(t *testing.T) {
	t.Parallel()

	code, payload := serveReady(t, GinHandler(t.Context(), nil))

	if code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
	}

	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
}

func TestPick_FirstNonNil(t *testing.T) {
	t.Parallel()

	if got := Pick(); got != nil {
		t.Fatalf("Pick() = %v, want nil", got)
	}

	probe := sharedreadiness.NewProbe("bot")
	if got := Pick(nil, probe); got != probe {
		t.Fatal("Pick(nil, probe) did not return probe")
	}
}

func TestDependencyChecks_NilClientsFailClosed(t *testing.T) {
	t.Parallel()

	if err := sharedreadiness.PostgresCheck(nil).Probe(t.Context()); err == nil {
		t.Fatal("PostgresCheck(nil) probe error = nil, want non-nil")
	}

	if err := sharedreadiness.ValkeyCheck(nil).Probe(t.Context()); err == nil {
		t.Fatal("ValkeyCheck(nil) probe error = nil, want non-nil")
	}
}

func serveReady(t *testing.T, handler gin.HandlerFunc) (statusCode int, payload map[string]any) {
	t.Helper()

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.GET("/ready", handler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("/ready JSON 파싱 실패: %v, raw=%s", err, rec.Body.String())
	}

	return rec.Code, payload
}
