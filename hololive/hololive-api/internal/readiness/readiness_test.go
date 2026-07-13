package readiness

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
)

func okCheck(name string) Check {
	return Check{Name: name, Probe: func(context.Context) error { return nil }}
}

func failCheck(name string, err error) Check {
	return Check{Name: name, Probe: func(context.Context) error { return err }}
}

func TestProbeEvaluate_AllHealthyReady(t *testing.T) {
	t.Parallel()

	code, payload := NewProbe("bot", okCheck("postgres"), okCheck("valkey")).Evaluate(t.Context())

	if code != http.StatusOK {
		t.Fatalf("Evaluate status = %d, want %d", code, http.StatusOK)
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

func TestProbeEvaluate_DependencyDownNotReady(t *testing.T) {
	t.Parallel()

	code, payload := NewProbe("admin",
		okCheck("postgres"),
		failCheck("valkey", errors.New("connection refused")),
	).Evaluate(t.Context())

	if code != http.StatusServiceUnavailable {
		t.Fatalf("Evaluate status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
	deps, ok := payload["dependencies"].(map[string]bool)
	if !ok {
		t.Fatalf("dependencies type = %T, want map[string]bool", payload["dependencies"])
	}
	if deps["valkey"] {
		t.Fatalf("valkey availability = true, want false")
	}
	if !deps["postgres"] {
		t.Fatalf("postgres availability = false, want true")
	}
}

func TestProbeEvaluate_HangingDependencyBoundedByTimeout(t *testing.T) {
	t.Parallel()

	probe := NewProbe("llm", Check{
		Name: "postgres",
		Probe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	probe.timeout = 50 * time.Millisecond

	start := time.Now()
	code, payload := probe.Evaluate(t.Context())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Evaluate did not bound a hanging probe: elapsed %v", elapsed)
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("Evaluate status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
}

func TestGinHandler_HealthyReturns200(t *testing.T) {
	t.Parallel()

	code, payload := serveReady(t, GinHandler(t.Context(), NewProbe("bot", okCheck("postgres"))))

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

	code, payload := serveReady(t, GinHandler(t.Context(), NewProbe("bot",
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
	probe := NewProbe("bot")
	if got := Pick(nil, probe); got != probe {
		t.Fatalf("Pick(nil, probe) did not return probe")
	}
}

func TestDependencyChecks_NilClientsFailClosed(t *testing.T) {
	t.Parallel()

	if err := PostgresCheck(nil).Probe(t.Context()); err == nil {
		t.Fatal("PostgresCheck(nil) probe error = nil, want non-nil")
	}
	if err := ValkeyCheck(nil).Probe(t.Context()); err == nil {
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

	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("/ready JSON 파싱 실패: %v, raw=%s", err, rec.Body.String())
	}
	return rec.Code, payload
}
