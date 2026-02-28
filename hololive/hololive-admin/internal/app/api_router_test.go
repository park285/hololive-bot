package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-admin/internal/server"
	"github.com/kapu/hololive-shared/pkg/config"
)

func TestFailClosedAuth(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	apiHandler := &server.APIHandler{}
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

			router, err := ProvideAPIRouter(ctx, cfg, logger, apiHandler, authHandler, nil, nil)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ProvideAPIRouter() expected error, but got nil")
				} else if err.Error() != tt.expectedErr {
					t.Errorf("ProvideAPIRouter() expected error %q, but got %q", tt.expectedErr, err.Error())
				}
				if router != nil {
					t.Errorf("ProvideAPIRouter() expected nil router on error, but got non-nil")
				}
			} else {
				if err != nil {
					t.Errorf("ProvideAPIRouter() unexpected error: %v", err)
				}
				if router == nil {
					t.Errorf("ProvideAPIRouter() expected non-nil router, but got nil")
				}
			}
		})
	}
}

func TestAPIRouter_CORSOriginGuard(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	apiHandler := &server.APIHandler{}
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		Telemetry: config.TelemetryConfig{
			Environment: "production",
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"https://allowed.example.com"},
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, apiHandler, authHandler, nil, nil)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() error = %v", err)
	}

	disallowedReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	disallowedReq.Header.Set("Origin", "https://blocked.example.com")
	disallowedRec := httptest.NewRecorder()
	router.ServeHTTP(disallowedRec, disallowedReq)
	if disallowedRec.Code != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %d, want %d", disallowedRec.Code, http.StatusForbidden)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	allowedReq.Header.Set("Origin", "https://allowed.example.com")
	allowedRec := httptest.NewRecorder()
	router.ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("allowed origin status = %d, want %d", allowedRec.Code, http.StatusOK)
	}
}

func TestAPIRouter_CORSProductionMissingOriginsDoesNotFailRouter(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	apiHandler := &server.APIHandler{}
	authHandler := &server.AuthHandler{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
		Telemetry: config.TelemetryConfig{
			Environment: "production",
		},
		CORS: config.CORSConfig{
			AllowedOrigins:      nil,
			Enforce:             true,
			MissingInProduction: true,
		},
	}

	router, err := ProvideAPIRouter(ctx, cfg, logger, apiHandler, authHandler, nil, nil)
	if err != nil {
		t.Fatalf("ProvideAPIRouter() unexpected error: %v", err)
	}
	if router == nil {
		t.Fatalf("ProvideAPIRouter() expected non-nil router")
	}
}
