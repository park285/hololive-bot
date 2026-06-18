package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func TestNewMetricsServerServesPrometheusTextWithAPIKey(t *testing.T) {
	server := NewMetricsServer("127.0.0.1:0", "test-key")

	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("Addr = %q, want 127.0.0.1:0", server.Addr)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	req.Header.Set(middleware.APIKeyHeader, "test-key")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "go_goroutines") {
		t.Fatalf("body missing go_goroutines:\n%.300s", body)
	}
}

func TestNewMetricsServerRejectsMissingAndWrongAPIKey(t *testing.T) {
	server := NewMetricsServer("127.0.0.1:0", "test-key")

	missing := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	missingRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d, want %d", missingRecorder.Code, http.StatusUnauthorized)
	}

	wrong := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	wrong.Header.Set(middleware.APIKeyHeader, "wrong-key")
	wrongRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongRecorder, wrong)
	if wrongRecorder.Code != http.StatusForbidden {
		t.Fatalf("wrong key status = %d, want %d", wrongRecorder.Code, http.StatusForbidden)
	}
}

func TestNewMetricsServerExposesOnlyMetricsRoute(t *testing.T) {
	server := NewMetricsServer("127.0.0.1:0", "")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("/health status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestNewRuntimeHTTPServersBuildsMetricsServerFromConfig(t *testing.T) {
	certFile, keyFile := writeH3LocalhostCertificate(t)
	servers, err := NewRuntimeHTTPServers(&config.ServerConfig{
		Port:           30001,
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
		MetricsAddr:    "127.0.0.1:0",
	}, http.NotFoundHandler(), "test.http")
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if servers.Metrics == nil {
		t.Fatal("Metrics = nil, want server")
	}

	noMetrics, err := NewRuntimeHTTPServers(&config.ServerConfig{
		Port:           30001,
		APIKey:         "test-key",
		HTTPTransports: []string{"h3"},
		H3Addr:         "127.0.0.1:0",
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
	}, http.NotFoundHandler(), "test.http")
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}
	if noMetrics.Metrics != nil {
		t.Fatal("Metrics != nil with empty MetricsAddr")
	}
}
