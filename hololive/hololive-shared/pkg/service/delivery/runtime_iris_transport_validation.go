package delivery

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const (
	runtimeIrisSchemeHTTP  = "http"
	runtimeIrisSchemeHTTPS = "https"
)

func validateRuntimeIrisTransportScheme(transport string, parsed *url.URL) error {
	if err := validateRuntimeIrisTransportURLScheme(transport, parsed.Scheme); err != nil {
		return fmt.Errorf("validate runtime iris transport URL scheme: %w", err)
	}

	if isRuntimeIrisProductionTransport(transport) {
		return nil
	}

	if !isRuntimeIrisDiagnosticTransport(transport) {
		return fmt.Errorf("unsupported IRIS_TRANSPORT: %s", transport)
	}

	if isRuntimeIrisLoopbackHost(parsed.Hostname()) {
		return nil
	}

	return fmt.Errorf("IRIS_TRANSPORT=%s is supported only for loopback diagnostics; production Iris egress requires h3", transport)
}

func validateRuntimeIrisTransportURLScheme(transport, scheme string) error {
	requiredScheme, ok := runtimeIrisTransportRequiredSchemes()[transport]
	if !ok || scheme == requiredScheme {
		return nil
	}

	return fmt.Errorf("IRIS_TRANSPORT=%s requires %s IRIS_BASE_URL, got %s", transport, requiredScheme, scheme)
}

func isRuntimeIrisProductionTransport(transport string) bool {
	return transport == "" || transport == "h3"
}

func isRuntimeIrisDiagnosticTransport(transport string) bool {
	return transport == "http1"
}

func isRuntimeIrisLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func runtimeIrisTransportRequiredSchemes() map[string]string {
	return map[string]string{
		"":   runtimeIrisSchemeHTTPS,
		"h3": runtimeIrisSchemeHTTPS,
	}
}

func runtimeIrisValidationTransport(explicit string) string {
	if transport := normalizeRuntimeIrisTransport(explicit); transport != "" {
		return transport
	}

	return normalizeRuntimeIrisTransport(os.Getenv("IRIS_TRANSPORT"))
}

func normalizeRuntimeIrisTransport(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if isRuntimeIrisHTTP3Transport(normalized) {
		return "h3"
	}

	if normalized == "http1" || normalized == "http" || normalized == "http/1.1" {
		return "http1"
	}

	return normalized
}

func isRuntimeIrisHTTP3Transport(normalized string) bool {
	return normalized == "h3" || normalized == "http3" || normalized == "http/3" || normalized == "quic"
}
