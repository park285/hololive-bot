package workerapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	workerreadiness "github.com/kapu/hololive-alarm-worker/internal/readiness"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestAlarmWorkerReadyProbeRequiresYouTubeDispatcherFlag(t *testing.T) {
	t.Setenv("ALARM_WORKER_EGRESS_LEASE_ENABLED", "true")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{PingFunc: func(context.Context) error { return nil }},
		Cache:    &cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }},
	}

	rec := serveAlarmWorkerReady(t, workerreadiness.InternalGinHandler(t.Context(), newAlarmWorkerReadyProbe(infra)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	payload := decodeReadyPayload(t, rec)
	flags := payloadObject(t, payload, "egress_flags")
	if flags["youtube_outbox_dispatcher_enabled"] != false {
		t.Fatalf("egress_flags = %v, want YouTube dispatcher false", flags)
	}
}

func TestAlarmWorkerReadyProbeReportsReadyWhenDependenciesAndFlagsReady(t *testing.T) {
	t.Setenv("ALARM_WORKER_EGRESS_LEASE_ENABLED", "true")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{PingFunc: func(context.Context) error { return nil }},
		Cache:    &cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }},
	}

	rec := serveAlarmWorkerReady(t, workerreadiness.InternalGinHandler(t.Context(), newAlarmWorkerReadyProbe(infra)))

	if rec.Code != http.StatusOK {
		t.Fatalf("/internal/ready status = %d, want %d", rec.Code, http.StatusOK)
	}
	payload := decodeReadyPayload(t, rec)
	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
}

func serveAlarmWorkerReady(t *testing.T, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/internal/ready", handler)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/ready", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeReadyPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode readiness payload: %v raw=%s", err, rec.Body.String())
	}
	return payload
}

func payloadObject(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want object", key, payload[key])
	}
	return value
}
