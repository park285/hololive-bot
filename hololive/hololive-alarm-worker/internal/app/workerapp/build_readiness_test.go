package workerapp

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	workerreadiness "github.com/kapu/hololive-alarm-worker/internal/readiness"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestAlarmWorkerReadyProbeRequiresPostgres(t *testing.T) {
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{PingFunc: func(context.Context) error { return errors.New("unavailable") }},
		Cache:    &cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }},
	}

	rec := serveAlarmWorkerReady(t, workerreadiness.InternalGinHandler(t.Context(), newAlarmWorkerReadyProbe(infra)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	payload := decodeReadyPayload(t, rec)
	dependencies := payloadObject(t, payload, "dependencies")

	if dependencies["postgres"] != false {
		t.Fatalf("dependencies = %v, want PostgreSQL false", dependencies)
	}
}

func TestAlarmWorkerReadyProbeReportsReadyWhenDependenciesAndFlagsReady(t *testing.T) {
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

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
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
