package settings

import (
	"strings"
	"testing"
)

func TestServerConfigTransportEnabled_EmptyTransports_DefaultH3(t *testing.T) {
	t.Parallel()

	s := &ServerConfig{}
	if !s.TransportEnabled("h3") {
		t.Fatal("empty HTTPTransports should default to h3 enabled")
	}
}

func TestServerConfigTransportEnabled_InvalidName(t *testing.T) {
	t.Parallel()

	s := &ServerConfig{}
	if s.TransportEnabled("grpc") {
		t.Fatal("invalid transport name should not be enabled")
	}
}

func TestServerConfigTransportEnabled_ExplicitList(t *testing.T) {
	t.Parallel()

	s := &ServerConfig{HTTPTransports: []string{"h3"}}
	if !s.TransportEnabled("http3") {
		t.Fatal("http3 alias should match h3")
	}

	if !s.TransportEnabled("quic") {
		t.Fatal("quic alias should match h3")
	}
}

func TestValidateServerTransports_RejectsExplicitEmptyTransport(t *testing.T) {
	t.Parallel()

	err := ValidateServerTransports(&ServerConfig{HTTPTransports: []string{""}})
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_HTTP_TRANSPORTS must include h3") {
		t.Fatalf("ValidateServerTransports(empty explicit) error = %v, want h3 required", err)
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
