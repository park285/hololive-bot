package readiness

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/health"
)

func okCheck(name string) Check {
	return Check{Name: name, Probe: func(context.Context) error { return nil }}
}

func failCheck(name string, err error) Check {
	return Check{Name: name, Probe: func(context.Context) error { return err }}
}

func TestNewProbeFiltersAndNormalizesChecks(t *testing.T) {
	t.Parallel()

	probe := NewProbe("  bot  ",
		Check{Name: "nil-probe"},
		Check{Name: "   ", Probe: func(context.Context) error { return nil }},
		Check{Name: "  postgres  ", Group: "unknown", Probe: func(context.Context) error { return nil }},
		Check{Name: "flag", Group: GroupEgressFlags, Probe: func(context.Context) error { return nil }},
	)

	if probe.Name() != "bot" {
		t.Fatalf("Name() = %q, want %q", probe.Name(), "bot")
	}
	ready, groups := probe.Evaluate(t.Context())
	if !ready {
		t.Fatalf("Evaluate ready = false, want true")
	}
	if got := groups[GroupDependencies]; len(got) != 1 || !got["postgres"] {
		t.Fatalf("dependencies = %v, want only trimmed postgres", got)
	}
	if got := groups[GroupEgressFlags]; len(got) != 1 || !got["flag"] {
		t.Fatalf("egress_flags = %v, want only flag", got)
	}
}

func TestEvaluateSeedsStandardGroupsEvenWhenEmpty(t *testing.T) {
	t.Parallel()

	ready, groups := NewProbe("bot").Evaluate(t.Context())

	if !ready {
		t.Fatalf("Evaluate ready = false, want true")
	}
	deps, ok := groups[GroupDependencies]
	if !ok || len(deps) != 0 {
		t.Fatalf("dependencies group = %v (present=%v), want present and empty", deps, ok)
	}
	flags, ok := groups[GroupEgressFlags]
	if !ok || len(flags) != 0 {
		t.Fatalf("egress_flags group = %v (present=%v), want present and empty", flags, ok)
	}
}

func TestEvaluateFailingCheckNotReady(t *testing.T) {
	t.Parallel()

	ready, groups := NewProbe("bot",
		okCheck("postgres"),
		failCheck("valkey", errors.New("connection refused")),
	).Evaluate(t.Context())

	if ready {
		t.Fatalf("Evaluate ready = true, want false")
	}
	if !groups[GroupDependencies]["postgres"] {
		t.Fatalf("postgres = false, want true")
	}
	if groups[GroupDependencies]["valkey"] {
		t.Fatalf("valkey = true, want false")
	}
}

func TestEvaluateHangingCheckBoundedByTimeout(t *testing.T) {
	t.Parallel()

	probe := NewProbe("bot", Check{
		Name: "postgres",
		Probe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	probe.timeout = 50 * time.Millisecond

	start := time.Now()
	ready, _ := probe.Evaluate(t.Context())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Evaluate did not bound a hanging probe: elapsed %v", elapsed)
	}
	if ready {
		t.Fatalf("Evaluate ready = true, want false")
	}
}

func TestDependencyChecksNilClientsFailClosed(t *testing.T) {
	t.Parallel()

	postgres := PostgresCheck(nil)
	if postgres.Group != GroupDependencies {
		t.Fatalf("PostgresCheck group = %q, want %q", postgres.Group, GroupDependencies)
	}
	if err := postgres.Probe(t.Context()); err == nil {
		t.Fatal("PostgresCheck(nil) probe error = nil, want non-nil")
	}
	valkey := ValkeyCheck(nil)
	if valkey.Group != GroupDependencies {
		t.Fatalf("ValkeyCheck group = %q, want %q", valkey.Group, GroupDependencies)
	}
	if err := valkey.Probe(t.Context()); err == nil {
		t.Fatal("ValkeyCheck(nil) probe error = nil, want non-nil")
	}
}

func TestHTTPStatus(t *testing.T) {
	t.Parallel()

	if code, status := HTTPStatus(true); code != http.StatusOK || status != "ready" {
		t.Fatalf("HTTPStatus(true) = (%d, %q), want (%d, ready)", code, status, http.StatusOK)
	}
	if code, status := HTTPStatus(false); code != http.StatusServiceUnavailable || status != "not_ready" {
		t.Fatalf("HTTPStatus(false) = (%d, %q), want (%d, not_ready)", code, status, http.StatusServiceUnavailable)
	}
}

func TestBasePayloadCarriesHealthFields(t *testing.T) {
	t.Parallel()

	base := health.Response{Status: "ok", Version: "v1", Uptime: "1s", Goroutines: 7}
	payload := BasePayload(base, "ready")

	if len(payload) != 4 {
		t.Fatalf("payload keys = %d, want 4: %v", len(payload), payload)
	}
	if payload["status"] != "ready" || payload["version"] != "v1" || payload["uptime"] != "1s" || payload["goroutines"] != 7 {
		t.Fatalf("payload = %v, want status/version/uptime/goroutines", payload)
	}
}

func TestRequestContextPrefersRequestThenFallback(t *testing.T) {
	t.Parallel()

	fallback := t.Context()
	if got := RequestContext(fallback, nil); got != fallback {
		t.Fatalf("RequestContext(fallback, nil) did not return fallback")
	}
	var nilFallback context.Context
	if got := RequestContext(nilFallback, nil); got == nil {
		t.Fatalf("RequestContext(nil, nil) = nil, want background")
	}

	gin.SetMode(gin.ReleaseMode)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Request = httptest.NewRequestWithContext(fallback, http.MethodGet, "/ready", http.NoBody)
	if got := RequestContext(context.Background(), ginCtx); got != ginCtx.Request.Context() {
		t.Fatalf("RequestContext did not prefer request context")
	}
}
