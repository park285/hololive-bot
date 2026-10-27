package settings

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateServerTransports: h3 전용 전송 계약과 인증서 파일 요구를 확인한다.
func ValidateServerTransports(server *ServerConfig) error {
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
	transport := strings.TrimSpace(strings.ToLower(raw))
	if transport == "" {
		return "", true
	}

	if IsHTTP3Transport(transport) {
		return "h3", true
	}

	return transport, false
}

// IsHTTP3Transport는 HTTP/3 전송 별칭(h3·http3·http/3·quic)을 대소문자·앞뒤 공백과 무관하게 판정한다.
// 서버 전송 목록과 Iris 런타임 전송 설정이 같은 별칭 집합을 쓴다.
func IsHTTP3Transport(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "h3", "http3", "http/3", "quic":
		return true
	default:
		return false
	}
}
