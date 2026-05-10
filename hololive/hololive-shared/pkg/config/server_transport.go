package config

import (
	"fmt"
	"strings"
)

func (c *Config) validateServerTransports() error {
	for _, rawTransport := range c.Server.HTTPTransports {
		if _, ok := normalizeServerHTTPTransport(rawTransport); !ok {
			return fmt.Errorf("unsupported HOLOLIVE_HTTP_TRANSPORTS value: %s", rawTransport)
		}
	}

	if c.ServerTransportEnabled("h3") {
		if strings.TrimSpace(c.Server.H3Addr) == "" {
			return fmt.Errorf("HOLOLIVE_H3_ADDR is required when h3 transport is enabled")
		}
		if strings.TrimSpace(c.Server.H3CertFile) == "" {
			return fmt.Errorf("HOLOLIVE_H3_CERT_FILE is required when h3 transport is enabled")
		}
		if strings.TrimSpace(c.Server.H3KeyFile) == "" {
			return fmt.Errorf("HOLOLIVE_H3_KEY_FILE is required when h3 transport is enabled")
		}
	}
	return nil
}

func (c *Config) ServerTransportEnabled(name string) bool {
	target, ok := normalizeServerHTTPTransport(name)
	if !ok || target == "" {
		return false
	}
	if len(c.Server.HTTPTransports) == 0 {
		return target == "h3"
	}
	for _, transport := range c.Server.HTTPTransports {
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
	case "h2c":
		return "h2c", true
	case "h3", "http3", "http/3", "quic":
		return "h3", true
	default:
		return transport, false
	}
}
