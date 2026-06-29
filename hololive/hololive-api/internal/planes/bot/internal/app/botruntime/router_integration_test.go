// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-api/internal/readiness"
	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestProvideHealthOnlyRouter_Integration(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	router, err := sharedserver.NewHealthOnlyRuntimeRouter(t.Context(), logger, "test-key")
	if err != nil {
		t.Fatalf("NewHealthOnlyRuntimeRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	resp := getRouterTestResponse(t, server.URL+"/health")

	require.NoError(t, resp.Body.Close())

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	readyResp := getRouterTestResponse(t, server.URL+"/ready")

	require.NoError(t, readyResp.Body.Close())

	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", readyResp.StatusCode, http.StatusOK)
	}

	metricsReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/metrics", http.NoBody)
	if err != nil {
		t.Fatalf("new /metrics request error = %v", err)
	}

	metricsReq.Header.Set(middleware.APIKeyHeader, "test-key")

	metricsResp, err := http.DefaultClient.Do(metricsReq)
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	require.NotNil(t, metricsResp)

	require.NoError(t, metricsResp.Body.Close())

	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", metricsResp.StatusCode, http.StatusOK)
	}
}

func TestProvideBotRouter_Integration(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	router, err := ProvideBotRouter(t.Context(), &config.Config{}, logger, nil, nil)
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	resp := getRouterTestResponse(t, server.URL+"/health")

	require.NoError(t, resp.Body.Close())

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	readyResp := getRouterTestResponse(t, server.URL+"/ready")

	require.NoError(t, readyResp.Body.Close())

	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", readyResp.StatusCode, http.StatusOK)
	}
}

func TestProvideBotRouter_DependencyReadyProbeIsInternalOnly(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	probeCalls := 0
	probe := readiness.NewProbe("bot", readiness.Check{
		Name: "postgres",
		Probe: func(context.Context) error {
			probeCalls++
			return errors.New("pool unavailable")
		},
	})

	router, err := ProvideBotRouter(t.Context(), &config.Config{
		Server: config.ServerConfig{APIKey: "test-key"},
	}, logger, nil, nil, probe)
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	readyResp := getRouterTestResponse(t, server.URL+"/ready")
	require.NoError(t, readyResp.Body.Close())
	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", readyResp.StatusCode, http.StatusOK)
	}
	if probeCalls != 0 {
		t.Fatalf("/ready invoked dependency probe %d time(s), want 0", probeCalls)
	}

	internalReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/internal/ready", http.NoBody)
	if err != nil {
		t.Fatalf("build /internal/ready request: %v", err)
	}
	internalReq.Header.Set(middleware.APIKeyHeader, "test-key")
	internalResp, err := http.DefaultClient.Do(internalReq)
	if err != nil {
		t.Fatalf("GET /internal/ready: %v", err)
	}
	require.NotNil(t, internalResp)
	require.NoError(t, internalResp.Body.Close())
	if internalResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/internal/ready status = %d, want %d", internalResp.StatusCode, http.StatusServiceUnavailable)
	}
	if probeCalls != 1 {
		t.Fatalf("/internal/ready invoked dependency probe %d time(s), want 1", probeCalls)
	}
}

func TestProvideBotRouter_FailsClosedWhenTriggerAPIKeyMissing(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := ProvideBotRouter(t.Context(), &config.Config{}, logger, nil, triggerHandler)
	if err == nil {
		t.Fatal("ProvideBotRouter() error = nil, want non-nil")
	}

	if router != nil {
		t.Fatal("ProvideBotRouter() router = non-nil, want nil")
	}

	if err.Error() != "API_SECRET_KEY required" {
		t.Fatalf("ProvideBotRouter() error = %q, want %q", err.Error(), "API_SECRET_KEY required")
	}
}

func getRouterTestResponse(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new GET request error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	require.NotNil(t, resp)
	return resp
}
