package httpclient

import (
	"crypto/tls"
	"testing"
)

func TestInsecureSkipVerifyPolicy(t *testing.T) {
	tests := []struct {
		name       string
		otelEnv    string
		allowInsec string
		wantInsec  bool
	}{
		{
			name:       "production with InsecureSkipVerify allowed - fail closed",
			otelEnv:    "production",
			allowInsec: "true",
			wantInsec:  false,
		},
		{
			name:       "default (production) with InsecureSkipVerify allowed - fail closed",
			otelEnv:    "",
			allowInsec: "true",
			wantInsec:  false,
		},
		{
			name:       "development with InsecureSkipVerify allowed",
			otelEnv:    "development",
			allowInsec: "true",
			wantInsec:  true,
		},
		{
			name:       "test with InsecureSkipVerify allowed",
			otelEnv:    "test",
			allowInsec: "true",
			wantInsec:  true,
		},
		{
			name:       "production without HTTP_ALLOW_INSECURE_TLS",
			otelEnv:    "production",
			allowInsec: "false",
			wantInsec:  false,
		},
		{
			name:       "production with empty HTTP_ALLOW_INSECURE_TLS",
			otelEnv:    "production",
			allowInsec: "",
			wantInsec:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_ENVIRONMENT", tt.otelEnv)
			t.Setenv("HTTP_ALLOW_INSECURE_TLS", tt.allowInsec)

			cfg := DefaultConfig()
			cfg.InsecureSkipVerify = true

			tr := NewTransport(cfg)
			if tr == nil || tr.TLSClientConfig == nil {
				t.Fatal("NewTransport returned nil transport or TLS config")
			}
			if tr.TLSClientConfig.InsecureSkipVerify != tt.wantInsec {
				t.Errorf("InsecureSkipVerify = %v, want %v", tr.TLSClientConfig.InsecureSkipVerify, tt.wantInsec)
			}
		})
	}
}

func TestInsecureSkipVerifyNotRequestedInProduction(t *testing.T) {
	tests := []struct {
		name    string
		otelEnv string
	}{
		{"production", "production"},
		{"development", "development"},
		{"test", "test"},
		{"default (production)", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_ENVIRONMENT", tt.otelEnv)

			cfg := DefaultConfig()
			tr := NewTransport(cfg)
			if tr == nil || tr.TLSClientConfig == nil {
				t.Fatal("NewTransport returned nil transport or TLS config")
			}
			if tr.TLSClientConfig.InsecureSkipVerify {
				t.Error("InsecureSkipVerify should remain disabled by default")
			}
		})
	}
}

func TestMinTLSVersionEnforced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinTLSVersion = tls.VersionTLS10

	tr := NewTransport(cfg)
	if tr == nil || tr.TLSClientConfig == nil {
		t.Fatal("NewTransport returned nil transport or TLS config")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want %v", tr.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}
