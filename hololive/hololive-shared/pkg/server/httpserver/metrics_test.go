package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestNewMetricsServerServesPrometheusTextWithAPIKey(t *testing.T) {
	server := NewMetricsServer(t.Context(), "127.0.0.1:0", "test-key")

	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("Addr = %q, want 127.0.0.1:0", server.Addr)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	req.Header.Set(common.APIKeyHeader, "test-key")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "go_goroutines") {
		t.Fatalf("body missing go_goroutines:\n%.300s", body)
	}
}

func TestNewRuntimeHTTPServersForwardsWorkerRegistryToMetricsServer(t *testing.T) {
	identity, err := workercontract.KnownIdentity("hololive", "youtube-collector")
	if err != nil {
		t.Fatal(err)
	}
	profilePath, err := filepath.Abs(filepath.Join("..", "..", "config", "settings", "testdata", "stack-worker-profile-youtube-collector.json"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := workercontract.LoadProfileFile(profilePath, identity)
	if err != nil {
		t.Fatal(err)
	}
	worker := loaded.Profile.Workers["collection"]
	worker.Executor.Enabled = false
	loaded.Profile.Workers["collection"] = worker
	registry := workercontract.NewRegistry(loaded, nil)
	if err := registry.Register(workercontract.Registration{
		WorkerID:                "collection",
		Runtime:                 workercontract.RuntimeGo,
		QueueBackend:            workercontract.QueueMemory,
		QueueScope:              workercontract.QueueScopeProcess,
		SettingsValidated:       true,
		PerJobDeadlineValidated: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Seal(); err != nil {
		t.Fatal(err)
	}

	certFile, keyFile := writeH3LocalhostCertificate(t)
	servers, err := NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		MetricsAddr:    "127.0.0.1:0",
	}, http.NotFoundHandler(), "test.http", registry)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	request.Header.Set(common.APIKeyHeader, "test-key")
	recorder := httptest.NewRecorder()
	servers.Metrics.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `iris_stack_worker_configured_workers{`) || !strings.Contains(body, `worker="collection"`) {
		t.Fatalf("metrics missing worker registry:\n%.500s", body)
	}
}

func TestNewMetricsServerRejectsMissingAndWrongAPIKey(t *testing.T) {
	server := NewMetricsServer(t.Context(), "127.0.0.1:0", "test-key")

	missing := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	missingRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d, want %d", missingRecorder.Code, http.StatusUnauthorized)
	}

	wrong := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	wrong.Header.Set(common.APIKeyHeader, "wrong-key")
	wrongRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongRecorder, wrong)
	if wrongRecorder.Code != http.StatusForbidden {
		t.Fatalf("wrong key status = %d, want %d", wrongRecorder.Code, http.StatusForbidden)
	}
}

func TestNewMetricsServerExposesOnlyMetricsRoute(t *testing.T) {
	server := NewMetricsServer(t.Context(), "127.0.0.1:0", "")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("/health status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestNewMetricsServerKeylessDeniedOnNonLoopback(t *testing.T) {
	ctx := context.Background()

	denied := []string{"0.0.0.0:30095", ":30095", "192.168.1.5:30095"}
	for _, addr := range denied {
		server := NewMetricsServer(t.Context(), addr, "")
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/metrics", http.NoBody))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("keyless metrics on %q status = %d, want %d", addr, rec.Code, http.StatusForbidden)
		}
	}

	allowed := []string{"127.0.0.1:30095", "[::1]:30095", "localhost:30095"}
	for _, addr := range allowed {
		server := NewMetricsServer(t.Context(), addr, "")
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/metrics", http.NoBody))
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless metrics on loopback %q status = %d, want %d", addr, rec.Code, http.StatusOK)
		}
	}
}

func TestNewRuntimeHTTPServersBuildsMetricsServerFromConfig(t *testing.T) {
	certFile, keyFile := writeH3LocalhostCertificate(t)
	servers, err := NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		Port:           30001,
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		MetricsAddr:    "127.0.0.1:0",
	}, http.NotFoundHandler(), "test.http", nil)
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if servers.Metrics == nil {
		t.Fatal("Metrics = nil, want server")
	}

	noMetrics, err := NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		Port:           30001,
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
	}, http.NotFoundHandler(), "test.http", nil)
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if noMetrics.Metrics != nil {
		t.Fatal("Metrics != nil with empty MetricsAddr")
	}
}
