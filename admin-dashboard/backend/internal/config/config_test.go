package config

import "testing"

func TestSecurityModeParse(t *testing.T) {
	if (&inputValues{values: map[string]string{"MODE": "monitor"}}).securityMode("MODE") != SecurityMonitor {
		t.Fatal("monitor must parse")
	}

	if (&inputValues{values: map[string]string{"MODE": "off"}}).securityMode("MODE") != SecurityOff {
		t.Fatal("off must parse")
	}

	in := &inputValues{values: map[string]string{"MODE": "bad"}}
	in.securityMode("MODE")

	if in.err == nil {
		t.Fatal("invalid mode must reject configuration")
	}
}

func TestValidateValkeyURL(t *testing.T) {
	if err := validateValkeyURL("redis://valkey-cache:6379"); err == nil {
		t.Fatal("scheme must fail")
	}

	if err := validateValkeyURL(":bad pass@valkey-cache:6379"); err == nil {
		t.Fatal("unsafe userinfo must fail")
	}

	if err := validateValkeyURL(":safe-pass@valkey-cache:6379"); err != nil {
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
