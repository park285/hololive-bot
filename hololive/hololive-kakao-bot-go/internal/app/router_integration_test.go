package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestProvideHealthOnlyRouter_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router, err := ProvideHealthOnlyRouter(context.Background(), logger)
	if err != nil {
		t.Fatalf("ProvideHealthOnlyRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	metricsResp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	metricsResp.Body.Close()
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", metricsResp.StatusCode, http.StatusOK)
	}
}

func TestProvideBotRouter_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router, err := ProvideBotRouter(context.Background(), &config.Config{}, logger, nil, nil)
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
