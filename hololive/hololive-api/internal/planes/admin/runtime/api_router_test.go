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

package adminruntime

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

const testAPIKey = "test-key"

func TestFailClosedAuth(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()

	tests := []struct {
		name        string
		apiKey      string
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "API Key provided - Success",
			apiKey:  testAPIKey,
			wantErr: false,
		},
		{
			name:        "API Key missing - Fail (Fail-Closed)",
			apiKey:      "",
			wantErr:     true,
			expectedErr: "API_SECRET_KEY required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appConfig := &apiconfig.AdminPlaneConfig{
				Server: settings.ServerConfig{
					APIKey: tt.apiKey,
				},
			}

			router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t))

			if tt.wantErr {
				assertAPIRouterError(t, router, err, tt.expectedErr)

				return
			}

			assertAPIRouterSuccess(t, router, err)
		})
	}
}

func assertAPIRouterError(t *testing.T, router *gin.Engine, err error, expectedErr string) {
	t.Helper()

	if err == nil {
		t.Fatal("ProvideAPIRouter() expected error, but got nil")
	}

	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("ProvideAPIRouter() expected error containing %q, but got %q", expectedErr, err.Error())
	}

	if router != nil {
		t.Error("ProvideAPIRouter() expected nil router on error, but got non-nil")
	}
}

func assertAPIRouterSuccess(t *testing.T, router *gin.Engine, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("ProvideAPIRouter() unexpected error: %v", err)
	}

	if router == nil {
		t.Fatal("ProvideAPIRouter() expected non-nil router, but got nil")
	}
}

// admin API는 Iris Console gateway가 서버 대 서버로 호출하므로 CORS 표면이 없다. Origin 헤더는 판정에 쓰지 않고
// 브라우저 허용 헤더도 내지 않는다.
func TestAPIRouter_IgnoresOriginWithoutCORSHeaders(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
		Environment: "production",
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t))
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	req.Header.Set("Origin", "https://any.example.com")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("origin request status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestProvideAPIRouter_NilDomainHandlers(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, nil, sharedtestutil.NewTestCacheService(ctx, t))
	if err == nil {
		t.Fatal("ProvideAPIRouter() expected error for nil domain handlers")
	}

	if !strings.Contains(err.Error(), "domain handlers must not be nil") {
		t.Fatalf("ProvideAPIRouter() error = %q, want it to contain %q", err.Error(), "domain handlers must not be nil")
	}

	if router != nil {
		t.Fatal("ProvideAPIRouter() expected nil router on error")
	}
}

func TestAPIRouter_StreamRoutesRequireAPIKey(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t))
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	tests := []struct {
		name       string
		path       string
		headerVal  string
		wantStatus int
	}{
		{name: "live missing api key", path: "/api/holo/streams/live?org=", wantStatus: http.StatusUnauthorized},
		{name: "live invalid api key", path: "/api/holo/streams/live?org=", headerVal: "wrong-key", wantStatus: http.StatusForbidden},
		{name: "live valid api key", path: "/api/holo/streams/live?org=", headerVal: testAPIKey, wantStatus: http.StatusBadRequest},
		{name: "upcoming missing api key", path: "/api/holo/streams/upcoming?org=", wantStatus: http.StatusUnauthorized},
		{name: "upcoming invalid api key", path: "/api/holo/streams/upcoming?org=", headerVal: "wrong-key", wantStatus: http.StatusForbidden},
		{name: "upcoming valid api key", path: "/api/holo/streams/upcoming?org=", headerVal: testAPIKey, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set(common.APIKeyHeader, tt.headerVal)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("path=%s status=%d want=%d body=%s", tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestAPIRouter_ProtectedRoutesStillRequireAPIKey(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t))
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/stats", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAPIRouter_MetricsRequireAPIKey(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.Handler{}
	domainHandlers := apiHandler.DomainHandlers()

	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			APIKey: testAPIKey,
		},
	}

	router, err := apphttp.ProvideAPIRouter(ctx, appConfig, logger, domainHandlers, sharedtestutil.NewTestCacheService(ctx, t))
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	tests := []struct {
		name       string
		headerVal  string
		wantStatus int
	}{
		{name: "missing api key", wantStatus: http.StatusUnauthorized},
		{name: "invalid api key", headerVal: "wrong-key", wantStatus: http.StatusForbidden},
		{name: "valid api key", headerVal: testAPIKey, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)

			if tt.headerVal != "" {
				req.Header.Set(common.APIKeyHeader, tt.headerVal)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
