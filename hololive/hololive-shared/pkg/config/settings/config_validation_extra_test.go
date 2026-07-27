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
			HTTPTransports: []string{"h3"},
		},
	}
	if !c.ServerTransportEnabled("http3") {
		t.Fatal("http3 alias should match h3")
	}
	if !c.ServerTransportEnabled("quic") {
		t.Fatal("quic alias should match h3")
	}
}

func TestValidateServerTransports_RejectsH2C(t *testing.T) {
	t.Parallel()

	err := validateServerTransports(&ServerConfig{HTTPTransports: []string{"h2c"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported HOLOLIVE_HTTP_TRANSPORTS value: h2c") {
		t.Fatalf("validateServerTransports(h2c) error = %v, want unsupported h2c", err)
	}
}

func TestValidateServerTransports_RejectsExplicitEmptyTransport(t *testing.T) {
	t.Parallel()

	err := validateServerTransports(&ServerConfig{HTTPTransports: []string{""}})
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_HTTP_TRANSPORTS must include h3") {
		t.Fatalf("validateServerTransports(empty explicit) error = %v, want h3 required", err)
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
		{"h2c", "h2c", false},
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

func setAdminAPIRuntimeEnv(t *testing.T) {
	t.Helper()
	clearRuntimeRoleEnv(t)
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")
	t.Setenv("SERVER_PORT", "30006")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://admin.example.com")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "")
	t.Setenv("IRIS_BOT_TOKEN", "")
	t.Setenv("IRIS_BASE_URL", "")
	t.Setenv("IRIS_BASE_URL_FILE", "")
	t.Setenv("YOUTUBE_API_KEY", "")
}

func TestLoadAdminAPIRuntime_BootsWithoutIrisEgressTokens(t *testing.T) {
	setAdminAPIRuntimeEnv(t)

	config, err := LoadAdminAPIRuntime()
	if err != nil {
		t.Fatalf("LoadAdminAPIRuntime() error = %v", err)
	}
	if config.Iris.WebhookToken != "" || config.Iris.BotToken != "" {
		t.Fatalf("Iris tokens = %q/%q, want empty", config.Iris.WebhookToken, config.Iris.BotToken)
	}

	server := newIrisRuntimeDiagnosticsServer(t, loadTestWorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testURLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("Load() error = %v, want IRIS_WEBHOOK_TOKEN is required", err)
	}
}

func TestLoadAdminAPIRuntime_DefaultEnforcesCORSOrigins_05c4a5ef(t *testing.T) {
	setAdminAPIRuntimeEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	_, err := LoadAdminAPIRuntime()
	if err == nil {
		t.Fatal("LoadAdminAPIRuntime() error = nil, want missing CORS_ALLOWED_ORIGINS error")
	}
	if !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAdminAPIRuntime_RequiresHolodexKey(t *testing.T) {
	setAdminAPIRuntimeEnv(t)
	t.Setenv("HOLODEX_API_KEY", "")
	t.Setenv("HOLODEX_API_KEY_1", "")

	_, err := LoadAdminAPIRuntime()
	if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY is required") {
		t.Fatalf("LoadAdminAPIRuntime() error = %v, want HOLODEX_API_KEY is required", err)
	}
}
