package settings

import (
	"strings"
	"testing"
)

func TestValidateScraperActiveActiveConfig_EnabledEmptyNamespace(t *testing.T) {
	t.Parallel()

	err := validateScraperActiveActiveConfig(ScraperActiveActiveConfig{
		Enabled:   true,
		Namespace: "",
	})
	if err == nil {
		t.Fatal("expected error for enabled active-active with empty namespace")
	}
	if !strings.Contains(err.Error(), "YOUTUBE_PRODUCER_LEASE_NAMESPACE") {
		t.Fatalf("error = %q, want YOUTUBE_PRODUCER_LEASE_NAMESPACE mention", err.Error())
	}
}

func TestValidateScraperActiveActiveConfig_Disabled(t *testing.T) {
	t.Parallel()

	err := validateScraperActiveActiveConfig(ScraperActiveActiveConfig{
		Enabled:   false,
		Namespace: "",
	})
	if err != nil {
		t.Fatalf("expected nil for disabled active-active, got %v", err)
	}
}

func TestValidateScraperSchedulerConfig_PartialZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config ScraperSchedulerConfig
		errSub string
	}{
		{
			name: "negative poll timeout",
			config: ScraperSchedulerConfig{
				PollTimeout:     -1,
				ErrorBackoffMin: 1,
				ErrorBackoffMax: 2,
			},
			errSub: "POLL_TIMEOUT",
		},
		{
			name: "negative backoff min",
			config: ScraperSchedulerConfig{
				PollTimeout:     1,
				ErrorBackoffMin: -1,
				ErrorBackoffMax: 2,
			},
			errSub: "BACKOFF_MIN",
		},
		{
			name: "negative backoff max",
			config: ScraperSchedulerConfig{
				PollTimeout:     1,
				ErrorBackoffMin: 1,
				ErrorBackoffMax: -1,
			},
			errSub: "BACKOFF_MAX",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateScraperSchedulerConfig(tt.config)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.errSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.errSub)
			}
		})
	}
}

func TestServerTransportEnabled_EmptyTransports_DefaultH3(t *testing.T) {
	t.Parallel()

	c := &Config{}
	if !c.ServerTransportEnabled("h3") {
		t.Fatal("empty HTTPTransports should default to h3 enabled")
	}
	if c.ServerTransportEnabled("h2c") {
		t.Fatal("empty HTTPTransports should not enable h2c")
	}
}

func TestServerTransportEnabled_InvalidName(t *testing.T) {
	t.Parallel()

	c := &Config{}
	if c.ServerTransportEnabled("grpc") {
		t.Fatal("invalid transport name should not be enabled")
	}
}

func TestServerTransportEnabled_ExplicitList(t *testing.T) {
	t.Parallel()

	c := &Config{
		Server: ServerConfig{
			HTTPTransports: []string{"h2c", "h3"},
		},
	}
	if !c.ServerTransportEnabled("h2c") {
		t.Fatal("h2c should be enabled when in list")
	}
	if !c.ServerTransportEnabled("http3") {
		t.Fatal("http3 alias should match h3")
	}
	if !c.ServerTransportEnabled("quic") {
		t.Fatal("quic alias should match h3")
	}
}

func TestNormalizeServerHTTPTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "", true},
		{"h2c", "h2c", true},
		{"h3", "h3", true},
		{"http3", "h3", true},
		{"http/3", "h3", true},
		{"quic", "h3", true},
		{"H3", "h3", true},
		{"grpc", "grpc", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, ok := normalizeServerHTTPTransport(tt.input)
			if ok != tt.ok {
				t.Fatalf("normalizeServerHTTPTransport(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("normalizeServerHTTPTransport(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidPostgresSSLMode(t *testing.T) {
	t.Parallel()

	valid := []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
	for _, mode := range valid {
		if !isValidPostgresSSLMode(mode) {
			t.Fatalf("isValidPostgresSSLMode(%q) = false, want true", mode)
		}
	}

	if isValidPostgresSSLMode("invalid") {
		t.Fatal("isValidPostgresSSLMode(\"invalid\") = true, want false")
	}
}
