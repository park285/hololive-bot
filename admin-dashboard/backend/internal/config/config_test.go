package config

import "testing"

func TestSecurityModeParse(t *testing.T) {
	if parseSecurityMode("monitor") != SecurityMonitor {
		t.Fatal("monitor must parse")
	}
	if parseSecurityMode("off") != SecurityOff {
		t.Fatal("off must parse")
	}
	if parseSecurityMode("bad") != SecurityEnforce {
		t.Fatal("invalid mode must enforce")
	}
}

func TestValidateValkeyURL(t *testing.T) {
	if _, err := validateValkeyURL("redis://valkey-cache:6379"); err == nil {
		t.Fatal("scheme must fail")
	}
	if _, err := validateValkeyURL(":bad pass@valkey-cache:6379"); err == nil {
		t.Fatal("unsafe userinfo must fail")
	}
	if _, err := validateValkeyURL(":safe-pass@valkey-cache:6379"); err != nil {
		t.Fatalf("safe userinfo should pass: %v", err)
	}
}

func TestSessionConfigValidation(t *testing.T) {
	cfg := DefaultSessionConfig()
	cfg.IdleWarningTimeout = cfg.IdleTimeout
	if err := cfg.Validate(); err == nil {
		t.Fatal("idle warning at timeout must fail")
	}
}
