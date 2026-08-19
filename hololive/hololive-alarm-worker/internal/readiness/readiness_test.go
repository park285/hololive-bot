package readiness

import (
	"context"
	"errors"
	"net/http"
	"testing"

	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestInternalResponseReportsDependencies(t *testing.T) {
	probe := sharedreadiness.NewProbe("alarm-worker", sharedreadiness.PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return nil }}), sharedreadiness.ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}))

	statusCode, payload := internalResponse(probe, t.Context())

	if statusCode != http.StatusOK {
		t.Fatalf("internalResponse status = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	dependencies := boolGroup(t, payload, "dependencies")
	if !dependencies["postgres"] || !dependencies["valkey"] {
		t.Fatalf("dependencies = %v, want postgres and valkey ready", dependencies)
	}
	if _, ok := payload["egress_flags"]; ok {
		t.Fatalf("internal response exposed retired egress flags: %v", payload)
	}
}

func TestInternalResponseNotReadyWhenDependencyFails(t *testing.T) {
	probe := sharedreadiness.NewProbe("alarm-worker", sharedreadiness.PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return errors.New("down") }}), sharedreadiness.ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}))

	statusCode, payload := internalResponse(probe, t.Context())

	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("internalResponse status = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["status"] != "not_ready" {
		t.Fatalf("status = %v, want not_ready", payload["status"])
	}
	dependencies := boolGroup(t, payload, "dependencies")
	if dependencies["postgres"] {
		t.Fatalf("dependencies = %v, want postgres not ready", dependencies)
	}
}

func TestPublicResponseOmitsDependencyAndFlagDetails(t *testing.T) {
	probe := sharedreadiness.NewProbe("alarm-worker", sharedreadiness.PostgresCheck(&databasemocks.Client{PingFunc: func(context.Context) error { return nil }}), sharedreadiness.ValkeyCheck(&cachemocks.Client{IsConnectedFunc: func(context.Context) bool { return true }}))

	statusCode, payload := publicResponse(probe, t.Context())

	if statusCode != http.StatusOK {
		t.Fatalf("publicResponse status = %d, want %d", statusCode, http.StatusOK)
	}
	if _, ok := payload["dependencies"]; ok {
		t.Fatalf("publicResponse exposed dependencies: %v", payload)
	}
	if _, ok := payload["egress_flags"]; ok {
		t.Fatalf("publicResponse exposed egress_flags: %v", payload)
	}
}

func boolGroup(t *testing.T, payload map[string]any, key string) map[string]bool {
	t.Helper()
	value, ok := payload[key].(map[string]bool)
	if !ok {
		t.Fatalf("%s = %T, want map[string]bool", key, payload[key])
	}
	return value
}
