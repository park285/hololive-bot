package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestInvalidInputsCannotSelectDefaults(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"PORT", "FORCE_HTTPS", "CSRF_MODE", "WS_ORIGIN_MODE", "SESSION_HEARTBEAT_INTERVAL_MS"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			in := &inputValues{values: map[string]string{key: "invalid-sensitive-value"}}
			if _, err := load(in); err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "invalid-sensitive-value") {
				t.Fatalf("invalid %s was accepted or leaked", key)
			}
		})
	}
}

func TestDurationOverflowRejected(t *testing.T) {
	t.Parallel()

	in := &inputValues{values: map[string]string{"SESSION_HEARTBEAT_INTERVAL_MS": "9223372036854775807"}}
	in.session()

	if in.err == nil {
		t.Fatal("overflow must not wrap into a valid duration")
	}
}

func TestSecretFileModeAndReplacement(t *testing.T) {
	t.Parallel()

	for _, mode := range []os.FileMode{0o400, 0o600, 0o640, 0o644, 0o660, 0o700, 0o040} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			path := writeSecretForTest(t, "secret", "synthetic-test-value")
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}

			_, err := readSecretFile(path)
			allowed := mode == 0o400 || mode == 0o600 || mode == 0o640

			if (err == nil) != allowed {
				t.Fatalf("mode %s: allowed=%v error=%v", mode, allowed, err)
			}
		})
	}

	path := writeSecretForTest(t, "secret", "original")

	info, err := statSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}

	replacement := filepath.Join(filepath.Dir(path), "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	if _, err := readVerifiedSecretFile(path, info); err == nil {
		t.Fatal("replacement after lstat must be rejected")
	}
}

func TestProductionTransportSecurity(t *testing.T) {
	t.Parallel()

	base := SecurityConfig{AllowedOrigins: []string{"https://admin.example.test"}, CSRFMode: SecurityEnforce, WSOriginMode: SecurityEnforce, ForceHTTPS: true}

	for _, mutate := range []func(*SecurityConfig){
		func(c *SecurityConfig) { c.ForceHTTPS = false },
		func(c *SecurityConfig) { c.AllowedOrigins = []string{"https://admin.example.test/path"} },
		func(c *SecurityConfig) { c.AllowedOrigins = []string{"https://user:pass@admin.example.test"} },
		func(c *SecurityConfig) { c.AllowedOrigins = []string{"http://admin.example.test"} },
	} {
		cfg := base
		mutate(&cfg)

		if err := validateSecurityConfig("production", cfg); err == nil {
			t.Fatal("unsafe production input was accepted")
		}
	}
}

func TestUnifiedLoadPreservesAliasesAndRejectsUnsafeProduction(t *testing.T) {
	t.Parallel()

	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-test-password"), minimumAdminPasswordBcryptCost)
	if err != nil {
		t.Fatal(err)
	}

	in := &inputValues{values: map[string]string{
		"ENV": "production", "ADMIN_PASS_BCRYPT": string(hash), "ADMIN_SECRET_KEY": strings.Repeat("s", 32),
		"HOLO_BOT_URL": "https://upstream.example.test", "API_SECRET_KEY": "synthetic-upstream-key",
		"ALLOWED_ORIGINS": "https://admin.example.test", "TRUST_FORWARDED_HEADERS": "true", "TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}}

	cfg, err := load(in)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.HoloAdminAPIURL != in.values["HOLO_BOT_URL"] || cfg.HoloBotAPIKey != in.values["API_SECRET_KEY"] || cfg.SessionSecret != in.values["ADMIN_SECRET_KEY"] || cfg.AdminPassHash != in.values["ADMIN_PASS_BCRYPT"] {
		t.Fatal("supported alias was not preserved")
	}

	for _, mutate := range []func(*Config){
		func(c *Config) { c.TrustedForwarders = false },
		func(c *Config) { c.TrustedProxyCIDRs = nil },
		func(c *Config) { c.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")} },
		func(c *Config) { c.Env = "prodution" },
		func(c *Config) { c.SessionSecret = strings.Repeat("s", 31) },
	} {
		candidate := *cfg
		mutate(&candidate)

		if err := candidate.Validate(); err == nil {
			t.Fatal("unsafe production config accepted")
		}
	}
}
