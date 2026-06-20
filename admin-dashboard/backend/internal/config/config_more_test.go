package config

import (
	"net"
	"testing"
	"time"
)

func TestHB04ParseTrustedProxyCIDRs_e8fc8b7d(t *testing.T) {
	t.Run("parses and skips blanks", func(t *testing.T) {
		got, err := parseTrustedProxyCIDRs("10.0.0.0/8, , 192.168.0.0/16")
		if err != nil {
			t.Fatalf("parseTrustedProxyCIDRs error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if !got[0].Contains(net.ParseIP("10.5.5.5")) {
			t.Fatal("first cidr must contain 10.5.5.5")
		}
	})
	t.Run("rejects malformed entry", func(t *testing.T) {
		if _, err := parseTrustedProxyCIDRs("not-a-cidr"); err == nil {
			t.Fatal("malformed CIDR must error")
		}
	})
	t.Run("empty input yields no cidrs", func(t *testing.T) {
		got, err := parseTrustedProxyCIDRs("")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
}

func TestIsLocalhostOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:5173", true},
		{"http://localhost", true},
		{"https://127.0.0.1:30190", true},
		{"localhost:8080", true},
		{"127.0.0.1", true},
		{"http://[::1]:5173", true},
		{"https://[::1]", true},
		{"http://admin.capu.blog", false},
		{"https://example.com:443", false},
		{"http://[2001:db8::1]:80", false},
		{"http://127.0.0.2:5173", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isLocalhostOrigin(tt.origin); got != tt.want {
			t.Errorf("isLocalhostOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestNormalizeEscapedBcryptHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"escaped 2a", "$$2a$$10$abcdefghijklmnopqrstuv", "$2a$10$abcdefghijklmnopqrstuv"},
		{"escaped 2b", "$$2b$$12$xyz", "$2b$12$xyz"},
		{"escaped 2y", "$$2y$$10$xyz", "$2y$10$xyz"},
		{"already normal 2a", "$2a$10$abcdefghijklmnopqrstuv", "$2a$10$abcdefghijklmnopqrstuv"},
		{"not bcrypt passthrough", "plain-text", "plain-text"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeEscapedBcryptHash(tt.in); got != tt.want {
				t.Fatalf("normalizeEscapedBcryptHash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSessionConfigValidateFailureBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SessionConfig)
	}{
		{"heartbeat below 1s", func(c *SessionConfig) { c.HeartbeatInterval = 500 * time.Millisecond }},
		{"expiry below 1m", func(c *SessionConfig) { c.ExpiryDuration = 30 * time.Second }},
		{"absolute not greater than expiry", func(c *SessionConfig) { c.AbsoluteTimeout = c.ExpiryDuration }},
		{"idle below 1m", func(c *SessionConfig) { c.IdleTimeout = 30 * time.Second }},
		{"idle warning at idle timeout", func(c *SessionConfig) { c.IdleWarningTimeout = c.IdleTimeout }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultSessionConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() = nil, want error for %s", tt.name)
			}
		})
	}
}

func TestSessionConfigValidateDefaultPasses(t *testing.T) {
	t.Parallel()
	cfg := DefaultSessionConfig()
	if err := (&cfg).Validate(); err != nil {
		t.Fatalf("DefaultSessionConfig().Validate() = %v, want nil", err)
	}
}

func TestValidateTTLWindowsFailureBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SessionConfig)
	}{
		{"ttl below 1s", func(c *SessionConfig) { c.IdleSessionTTL = 500 * time.Millisecond }},
		{"ttl at or above idle timeout", func(c *SessionConfig) { c.IdleSessionTTL = c.IdleTimeout }},
		{"absolute warning at absolute timeout", func(c *SessionConfig) { c.AbsoluteWarningWindow = c.AbsoluteTimeout }},
		{"rotation below grace", func(c *SessionConfig) { c.RotationInterval = c.GracePeriod - time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultSessionConfig()
			tt.mutate(&cfg)
			if err := cfg.validateTTLWindows(); err == nil {
				t.Fatalf("validateTTLWindows() = nil, want error for %s", tt.name)
			}
		})
	}
}

func TestValidateTTLWindowsDefaultPasses(t *testing.T) {
	t.Parallel()
	cfg := DefaultSessionConfig()
	if err := (&cfg).validateTTLWindows(); err != nil {
		t.Fatalf("validateTTLWindows() = %v, want nil", err)
	}
}

func TestAliasOrDefault(t *testing.T) {
	t.Run("returns default when none set", func(t *testing.T) {
		if got := aliasOrDefault("fallback", "AO_PRIMARY", "AO_SECONDARY"); got != "fallback" {
			t.Fatalf("aliasOrDefault = %q, want fallback", got)
		}
	})
	t.Run("primary wins", func(t *testing.T) {
		t.Setenv("AO_PRIMARY", "primary-val")
		t.Setenv("AO_SECONDARY", "secondary-val")
		if got := aliasOrDefault("fallback", "AO_PRIMARY", "AO_SECONDARY"); got != "primary-val" {
			t.Fatalf("aliasOrDefault = %q, want primary-val", got)
		}
	})
	t.Run("secondary used when primary empty", func(t *testing.T) {
		t.Setenv("AO_PRIMARY", "")
		t.Setenv("AO_SECONDARY", "secondary-val")
		if got := aliasOrDefault("fallback", "AO_PRIMARY", "AO_SECONDARY"); got != "secondary-val" {
			t.Fatalf("aliasOrDefault = %q, want secondary-val", got)
		}
	})
}

func TestRequiredAlias(t *testing.T) {
	t.Run("error when none set", func(t *testing.T) {
		if _, err := requiredAlias("RA_PRIMARY", "RA_SECONDARY"); err == nil {
			t.Fatal("requiredAlias error = nil, want error when no alias is set")
		}
	})
	t.Run("returns first non-empty alias", func(t *testing.T) {
		t.Setenv("RA_PRIMARY", "")
		t.Setenv("RA_SECONDARY", "found")
		got, err := requiredAlias("RA_PRIMARY", "RA_SECONDARY")
		if err != nil {
			t.Fatalf("requiredAlias error = %v", err)
		}
		if got != "found" {
			t.Fatalf("requiredAlias = %q, want found", got)
		}
	})
}
