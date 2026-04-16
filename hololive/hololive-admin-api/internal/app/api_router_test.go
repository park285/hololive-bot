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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/server/middleware"

	"github.com/kapu/hololive-admin-api/internal/server"
)

func TestFailClosedAuth(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	tests := []struct {
		name        string
		apiKey      string
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "API Key provided - Success",
			apiKey:  "test-key",
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
			cfg := &config.Config{
				Server: config.ServerConfig{
					APIKey: tt.apiKey,
				},
				CORS: config.CORSConfig{
					AllowedOrigins: []string{"http://localhost:3000"},
				},
			}

			router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)

			if tt.wantErr {
				if err == nil {
					t.Error("ProvideAPIRouter() expected error, but got nil")
				} else if err.Error() != tt.expectedErr {
					t.Errorf("ProvideAPIRouter() expected error %q, but got %q", tt.expectedErr, err.Error())
				}

				if router != nil {
					t.Error("ProvideAPIRouter() expected nil router on error, but got non-nil")
				}
			} else {
				if err != nil {
					t.Errorf("ProvideAPIRouter() unexpected error: %v", err)
				}

				if router == nil {
					t.Error("ProvideAPIRouter() expected non-nil router, but got nil")
				}
			}
		})
	}
}

func TestAPIRouter_CORSOriginGuard(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		Environment: "production",
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"https://allowed.example.com"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	disallowedReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	disallowedReq.Header.Set("Origin", "https://blocked.example.com")

	disallowedRec := httptest.NewRecorder()
	router.ServeHTTP(disallowedRec, disallowedReq)

	if disallowedRec.Code != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %d, want %d", disallowedRec.Code, http.StatusForbidden)
	}

	allowedReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	allowedReq.Header.Set("Origin", "https://allowed.example.com")

	allowedRec := httptest.NewRecorder()
	router.ServeHTTP(allowedRec, allowedReq)

	if allowedRec.Code != http.StatusOK {
		t.Fatalf("allowed origin status = %d, want %d", allowedRec.Code, http.StatusOK)
	}
}

func TestAPIRouter_CORSProductionMissingOriginsDoesNotFailRouter(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		Environment: "production",
		CORS: config.CORSConfig{
			AllowedOrigins:      nil,
			Enforce:             true,
			MissingInProduction: true,
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() unexpected error: %v", err)
	}

	if router == nil {
		t.Fatal("ProvideAPIRouter() expected non-nil router")
	}
}

func TestProvideAPIRouter_NilDomainHandlers(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, nil, authHandler, nil, nil, nil)
	if err == nil {
		t.Fatal("ProvideAPIRouter() expected error for nil domain handlers")
	}

	if err.Error() != "domain handlers must not be nil" {
		t.Fatalf("ProvideAPIRouter() error = %q, want %q", err.Error(), "domain handlers must not be nil")
	}

	if router != nil {
		t.Fatal("ProvideAPIRouter() expected nil router on error")
	}
}

func TestAPIRouter_StreamRoutesRequireAPIKey(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)
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
		{name: "live valid api key", path: "/api/holo/streams/live?org=", headerVal: "test-key", wantStatus: http.StatusBadRequest},
		{name: "upcoming missing api key", path: "/api/holo/streams/upcoming?org=", wantStatus: http.StatusUnauthorized},
		{name: "upcoming invalid api key", path: "/api/holo/streams/upcoming?org=", headerVal: "wrong-key", wantStatus: http.StatusForbidden},
		{name: "upcoming valid api key", path: "/api/holo/streams/upcoming?org=", headerVal: "test-key", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set(middleware.APIKeyHeader, tt.headerVal)
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
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)
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
	apiHandler := &server.APIHandler{}
	domainHandlers := apiHandler.DomainHandlers()
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, domainHandlers, authHandler, nil, nil, nil)
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
		{name: "valid api key", headerVal: "test-key", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)

			if tt.headerVal != "" {
				req.Header.Set(middleware.APIKeyHeader, tt.headerVal)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
