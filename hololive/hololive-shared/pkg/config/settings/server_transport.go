package settings

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) validateServerTransports() error {
	if err := validateServerTransports(&c.Server); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	return nil
}

func validateServerTransports(server *ServerConfig) error {
	if err := validateServerHTTPTransportNames(server); err != nil {
		return fmt.Errorf("validate server HTTP transport names: %w", err)
	}

	if !server.TransportEnabled("h3") {
		return errors.New("HOLOLIVE_HTTP_TRANSPORTS must include h3")
	}

	if err := validateH3TransportFiles(server); err != nil {
		return fmt.Errorf("validate H3 transport files: %w", err)
	}

	return nil
}

func validateServerHTTPTransportNames(server *ServerConfig) error {
	for _, rawTransport := range server.HTTPTransports {
		if _, ok := normalizeServerHTTPTransport(rawTransport); !ok {
			return fmt.Errorf("unsupported HOLOLIVE_HTTP_TRANSPORTS value: %s", rawTransport)
		}
	}

	return nil
}

func validateH3TransportFiles(server *ServerConfig) error {
	if strings.TrimSpace(server.H3Addr) == "" {
		return errors.New("HOLOLIVE_H3_ADDR is required when h3 transport is enabled")
	}

	if strings.TrimSpace(server.H3CertFile) == "" {
		return errors.New("HOLOLIVE_H3_CERT_FILE is required when h3 transport is enabled")
	}

	if strings.TrimSpace(server.H3KeyFile) == "" {
		return errors.New("HOLOLIVE_H3_KEY_FILE is required when h3 transport is enabled")
	}

	return nil
}

func (c *Config) ServerTransportEnabled(name string) bool {
	if c == nil {
		return false
	}

	return c.Server.TransportEnabled(name)
}

func (s *ServerConfig) TransportEnabled(name string) bool {
	target, ok := normalizeServerHTTPTransport(name)
	if !ok || target == "" {
		return false
	}

	if len(s.HTTPTransports) == 0 {
		return target == "h3"
	}

	for _, transport := range s.HTTPTransports {
		candidate, ok := normalizeServerHTTPTransport(transport)
		if ok && candidate == target {
			return true
		}
	}

	return false
}

func normalizeServerHTTPTransport(raw string) (string, bool) {
	switch transport := strings.TrimSpace(strings.ToLower(raw)); transport {
	case "":
		return "", true
	case "h3", "http3", "http/3", "quic":
		return "h3", true
	default:
		return transport, false
	}
}
