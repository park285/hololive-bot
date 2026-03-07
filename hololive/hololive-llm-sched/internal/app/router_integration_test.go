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
)

func TestProvideTriggerRouter_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := ProvideTriggerRouter(context.Background(), logger, triggerHandler, "")
	if err != nil {
		t.Fatalf("ProvideTriggerRouter() error = %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	healthResp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", healthResp.StatusCode, http.StatusOK)
	}

	triggerResp, err := http.Post(server.URL+triggercontracts.MemberNewsWeeklyPath, "application/json", nil)
	if err != nil {
		t.Fatalf("POST /internal/trigger/membernews-weekly error = %v", err)
	}
	triggerResp.Body.Close()
	if triggerResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("trigger status = %d, want %d", triggerResp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestProvideTriggerRouter_Integration_WithAPIKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	router, err := ProvideTriggerRouter(context.Background(), logger, triggerHandler, "test-key")
	if err != nil {
		t.Fatalf("ProvideTriggerRouter() error = %v", err)
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
	resp.Body.Close()
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
	respWithKey.Body.Close()
	if respWithKey.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status with API key = %d, want %d", respWithKey.StatusCode, http.StatusServiceUnavailable)
	}
}
