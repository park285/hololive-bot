package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestGoldenInternalReadyAllHealthy(t *testing.T) {
	t.Setenv("ALARM_WORKER_EGRESS_LEASE_ENABLED", "true")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")
	probe := NewProbe("alarm-worker",
		PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return nil }}),
		ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}),
		BoolEnvNotFalseCheck("notification_egress_lease_enabled", "ALARM_WORKER_EGRESS_LEASE_ENABLED", true),
		ExplicitTrueBoolEnvCheck("youtube_outbox_dispatcher_enabled", "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"),
	)

	code, body := serveReadyGolden(t, InternalGinHandler(t.Context(), probe))

	if code != http.StatusOK {
		t.Fatalf("/internal/ready status = %d, want %d", code, http.StatusOK)
	}
	want := `{"dependencies":{"postgres":true,"valkey":true},"egress_flags":{"notification_egress_lease_enabled":true,"youtube_outbox_dispatcher_enabled":true},"goroutines":0,"runtime":"alarm-worker","status":"ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/internal/ready body = %s, want %s", body, want)
	}
}

func TestGoldenInternalReadyDependencyDown(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")
	probe := NewProbe("alarm-worker",
		PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return errors.New("down") }}),
		ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}),
		ExplicitTrueBoolEnvCheck("youtube_outbox_dispatcher_enabled", "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"),
	)

	code, body := serveReadyGolden(t, InternalGinHandler(t.Context(), probe))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"dependencies":{"postgres":false,"valkey":true},"egress_flags":{"youtube_outbox_dispatcher_enabled":true},"goroutines":0,"runtime":"alarm-worker","status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/internal/ready body = %s, want %s", body, want)
	}
}

func TestGoldenInternalReadyFlagsOnlyKeepsEmptyDependenciesGroup(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "false")
	probe := NewProbe("alarm-worker",
		ExplicitTrueBoolEnvCheck("youtube_outbox_dispatcher_enabled", "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"),
	)

	code, body := serveReadyGolden(t, InternalGinHandler(t.Context(), probe))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"dependencies":{},"egress_flags":{"youtube_outbox_dispatcher_enabled":false},"goroutines":0,"runtime":"alarm-worker","status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/internal/ready body = %s, want %s", body, want)
	}
}

func TestGoldenInternalReadyNilProbe(t *testing.T) {
	code, body := serveReadyGolden(t, InternalGinHandler(t.Context(), nil))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"goroutines":0,"status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/internal/ready body = %s, want %s", body, want)
	}
}

func TestGoldenPublicReadyAllHealthy(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")
	probe := NewProbe("alarm-worker",
		PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return nil }}),
		ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}),
		ExplicitTrueBoolEnvCheck("youtube_outbox_dispatcher_enabled", "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"),
	)

	code, body := serveReadyGolden(t, PublicGinHandler(t.Context(), probe))

	if code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
	}
	want := `{"goroutines":0,"runtime":"alarm-worker","status":"ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/ready body = %s, want %s", body, want)
	}
}

func TestGoldenPublicReadyNilProbe(t *testing.T) {
	code, body := serveReadyGolden(t, PublicGinHandler(t.Context(), nil))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"goroutines":0,"status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/ready body = %s, want %s", body, want)
	}
}

func serveReadyGolden(t *testing.T, handler gin.HandlerFunc) (statusCode int, canonicalBody string) {
	t.Helper()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/ready", handler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code, canonicalizeGolden(t, rec.Body.Bytes())
}

func canonicalizeGolden(t *testing.T, raw []byte) string {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("golden unmarshal: %v, raw=%s", err, raw)
	}
	normalizeGoldenDynamicFields(t, payload)
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("golden marshal: %v", err)
	}
	return string(out)
}

func normalizeGoldenDynamicFields(t *testing.T, payload map[string]any) {
	t.Helper()

	for key, placeholder := range map[string]string{"version": "VERSION", "uptime": "UPTIME"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		if _, ok := value.(string); !ok {
			t.Fatalf("%s = %T, want string", key, value)
		}
		payload[key] = placeholder
	}
	if value, exists := payload["goroutines"]; exists {
		if _, ok := value.(float64); !ok {
			t.Fatalf("goroutines = %T, want number", value)
		}
		payload["goroutines"] = float64(0)
	}
}
