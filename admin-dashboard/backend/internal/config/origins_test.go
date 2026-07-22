package config

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoadRejectsMissingProductionOrigins(t *testing.T) {
	adminHash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	t.Setenv("ENV", "production")
	t.Setenv("ADMIN_PASS_HASH", string(adminHash))
	t.Setenv("SESSION_SECRET", "0123456789abcdef")
	t.Setenv("ALLOWED_ORIGINS", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing production ALLOWED_ORIGINS error")
	} else if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
		t.Fatalf("Load() error = %q, want ALLOWED_ORIGINS context", err)
	}
}

func TestProductionOriginsRequireExplicitNonLocalhost(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")

	cfg := LoadSecurityConfig("production", false)
	if len(cfg.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %v, want empty after production localhost filtering", cfg.AllowedOrigins)
	}
	if err := validateAllowedOrigins("production", cfg.AllowedOrigins); err == nil {
		t.Fatal("validateAllowedOrigins() error = nil, want production configuration error")
	} else if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
		t.Fatalf("validateAllowedOrigins() error = %q, want ALLOWED_ORIGINS context", err)
	}
}

func TestProductionOriginsDropLocalhostAndNormalizeExternalOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:30190, https://admin.example.com/")

	cfg := LoadSecurityConfig("production", false)
	if err := validateAllowedOrigins("production", cfg.AllowedOrigins); err != nil {
		t.Fatalf("validateAllowedOrigins() error = %v", err)
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "https://admin.example.com" {
		t.Fatalf("AllowedOrigins = %v, want [https://admin.example.com]", cfg.AllowedOrigins)
	}
}

func TestDevelopmentOriginsUseLocalhostFallbackOnly(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")

	cfg := LoadSecurityConfig("development", false)
	if len(cfg.AllowedOrigins) != 4 {
		t.Fatalf("AllowedOrigins length = %d, want 4", len(cfg.AllowedOrigins))
	}
	for _, origin := range cfg.AllowedOrigins {
		if !isLocalhostOrigin(origin) {
			t.Fatalf("fallback origin = %q, want localhost", origin)
		}
		if strings.Contains(origin, "capu.blog") {
			t.Fatalf("fallback origin = %q, deployment domain must not be hardcoded", origin)
		}
	}
}

func TestProductionOriginsAllowExplicitLocalhostEscapeHatch(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")

	cfg := LoadSecurityConfig("production", true)
	if err := validateAllowedOrigins("production", cfg.AllowedOrigins); err != nil {
		t.Fatalf("validateAllowedOrigins() error = %v", err)
	}
	if len(cfg.AllowedOrigins) == 0 {
		t.Fatal("AllowedOrigins is empty, want localhost fallback with explicit escape hatch")
	}
}
