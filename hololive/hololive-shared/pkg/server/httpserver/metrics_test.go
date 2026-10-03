package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

func TestNewMetricsServerServesPrometheusTextWithAPIKey(t *testing.T) {
	server := NewMetricsServer(t.Context(), testLoopbackAddr, "test-key")

	if server.Addr != testLoopbackAddr {
		t.Fatalf("Addr = %q, want 127.0.0.1:0", server.Addr)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
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
	loaded := settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")

	// executor를 끈 memory queue worker는 snapshot source 없이 등록되므로 profile의 worker 전체를 그렇게 등록한다.
	for workerID, worker := range loaded.Profile.Workers {
		worker.Executor.Enabled = false
		loaded.Profile.Workers[workerID] = worker
	}

	registry := workercontract.NewRegistry(loaded, nil)

	for workerID := range loaded.Profile.Workers {
		if registerErr := registry.Register(workercontract.Registration{
			WorkerID:                workerID,
			Runtime:                 workercontract.RuntimeGo,
			QueueBackend:            workercontract.QueueMemory,
			QueueScope:              workercontract.QueueScopeProcess,
			SettingsValidated:       true,
			PerJobDeadlineValidated: true,
		}); registerErr != nil {
			t.Fatal(registerErr)
		}
	}

	if sealErr := registry.Seal(); sealErr != nil {
		t.Fatal(sealErr)
	}

	certFile, keyFile := writeH3LocalhostCertificate(t)

	servers, err := NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         testLoopbackAddr,
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		MetricsAddr:    testLoopbackAddr,
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
	if !strings.Contains(body, `iris_stack_worker_configured_workers{`) || !strings.Contains(body, `worker="source_observation"`) {
		t.Fatalf("metrics missing worker registry:\n%.500s", body)
	}
}

func TestNewMetricsServerRejectsMissingAndWrongAPIKey(t *testing.T) {
	server := NewMetricsServer(t.Context(), testLoopbackAddr, "test-key")

	missing := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	missingRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(missingRecorder, missing)

	if missingRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d, want %d", missingRecorder.Code, http.StatusUnauthorized)
	}

	wrong := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	wrong.Header.Set(common.APIKeyHeader, "wrong-key")

	wrongRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongRecorder, wrong)

	if wrongRecorder.Code != http.StatusForbidden {
		t.Fatalf("wrong key status = %d, want %d", wrongRecorder.Code, http.StatusForbidden)
	}
}

func TestNewMetricsServerRejectsAdminKeyWhenMetricsKeyDiffers(t *testing.T) {
	server := NewMetricsServer(t.Context(), testLoopbackAddr, "metrics-key")

	adminRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	adminRequest.Header.Set(common.APIKeyHeader, "admin-key")

	adminRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(adminRecorder, adminRequest)

	if adminRecorder.Code != http.StatusForbidden {
		t.Fatalf("admin key status = %d, want %d", adminRecorder.Code, http.StatusForbidden)
	}

	metricsRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsRequest.Header.Set(common.APIKeyHeader, "metrics-key")

	metricsRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(metricsRecorder, metricsRequest)

	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("metrics key status = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}
}

func TestNewMetricsServerExposesOnlyMetricsRoute(t *testing.T) {
	server := NewMetricsServer(t.Context(), testLoopbackAddr, "")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("/health status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestNewMetricsServerKeylessDeniedOnNonLoopback(t *testing.T) {
	ctx := t.Context()

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
		H3Addr:         testLoopbackAddr,
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		MetricsAddr:    testLoopbackAddr,
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
		H3Addr:         testLoopbackAddr,
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
