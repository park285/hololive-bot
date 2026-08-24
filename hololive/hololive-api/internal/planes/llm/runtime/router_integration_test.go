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

package runtime

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func TestBuildTriggerRouter_Integration(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := buildTriggerRouter(t.Context(), logger, triggerHandler, "")
	if err == nil {
		t.Fatal("buildTriggerRouter() error = nil, want non-nil")
	}

	if router != nil {
		t.Fatal("buildTriggerRouter() router = non-nil, want nil")
	}

	if err.Error() != "runtime router: register routes: API_SECRET_KEY required" {
		t.Fatalf("buildTriggerRouter() error = %q, want %q", err.Error(), "runtime router: register routes: API_SECRET_KEY required")
	}
}

const routerIntegrationAPIKey = "test-key"

func requireTriggerRouterStatus(t *testing.T, method, url, apiKey string, wantStatus int) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, http.NoBody)
	require.NoError(t, err)

	if apiKey != "" {
		req.Header.Set(common.APIKeyHeader, apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, wantStatus, resp.StatusCode)
}

func TestBuildTriggerRouter_Integration_WithAPIKey(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := buildTriggerRouter(t.Context(), logger, triggerHandler, routerIntegrationAPIKey)
	if err != nil {
		t.Fatalf("buildTriggerRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	triggerURL := server.URL + triggercontracts.MemberNewsWeeklyPath
	metricsURL := server.URL + "/metrics"

	requireTriggerRouterStatus(t, http.MethodPost, triggerURL, "", http.StatusUnauthorized)
	requireTriggerRouterStatus(t, http.MethodPost, triggerURL, routerIntegrationAPIKey, http.StatusServiceUnavailable)
	requireTriggerRouterStatus(t, http.MethodGet, metricsURL, "", http.StatusUnauthorized)
	requireTriggerRouterStatus(t, http.MethodGet, metricsURL, routerIntegrationAPIKey, http.StatusOK)
}
