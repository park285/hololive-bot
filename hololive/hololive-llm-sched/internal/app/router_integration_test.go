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

package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestBuildTriggerRouter_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := buildTriggerRouter(context.Background(), logger, triggerHandler, "")
	if err == nil {
		t.Fatal("buildTriggerRouter() error = nil, want non-nil")
	}
	if router != nil {
		t.Fatal("buildTriggerRouter() router = non-nil, want nil")
	}
	if err.Error() != "API_SECRET_KEY required" {
		t.Fatalf("buildTriggerRouter() error = %q, want %q", err.Error(), "API_SECRET_KEY required")
	}
}

func TestBuildTriggerRouter_Integration_WithAPIKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := buildTriggerRouter(context.Background(), logger, triggerHandler, "test-key")
	if err != nil {
		t.Fatalf("buildTriggerRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	if err != nil {
		t.Fatalf("new request error = %v", err)
	}
	// 인증 헤더 미포함 -> 401
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST without API key error = %v", err)
	}
	require.NoError(t, resp.Body.Close())
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without API key = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	reqWithKey, err := http.NewRequest(http.MethodPost, server.URL+triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	if err != nil {
		t.Fatalf("new request with key error = %v", err)
	}
	reqWithKey.Header.Set(middleware.APIKeyHeader, "test-key")
	respWithKey, err := http.DefaultClient.Do(reqWithKey)
	if err != nil {
		t.Fatalf("POST with API key error = %v", err)
	}
	require.NoError(t, respWithKey.Body.Close())
	if respWithKey.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status with API key = %d, want %d", respWithKey.StatusCode, http.StatusServiceUnavailable)
	}

	metricsReq, err := http.NewRequest(http.MethodGet, server.URL+"/metrics", http.NoBody)
	if err != nil {
		t.Fatalf("new metrics request error = %v", err)
	}
	metricsResp, err := http.DefaultClient.Do(metricsReq)
	if err != nil {
		t.Fatalf("GET /metrics without API key error = %v", err)
	}
	require.NoError(t, metricsResp.Body.Close())
	if metricsResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/metrics status without API key = %d, want %d", metricsResp.StatusCode, http.StatusUnauthorized)
	}

	metricsReqWithKey, err := http.NewRequest(http.MethodGet, server.URL+"/metrics", http.NoBody)
	if err != nil {
		t.Fatalf("new metrics request with key error = %v", err)
	}
	metricsReqWithKey.Header.Set(middleware.APIKeyHeader, "test-key")
	metricsRespWithKey, err := http.DefaultClient.Do(metricsReqWithKey)
	if err != nil {
		t.Fatalf("GET /metrics with API key error = %v", err)
	}
	require.NoError(t, metricsRespWithKey.Body.Close())
	if metricsRespWithKey.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status with API key = %d, want %d", metricsRespWithKey.StatusCode, http.StatusOK)
	}
}
