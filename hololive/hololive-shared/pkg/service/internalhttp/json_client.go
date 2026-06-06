package internalhttp

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const (
	internalH3CACertFileEnv = "HOLOLIVE_INTERNAL_H3_CA_CERT_FILE"
	hololiveH3CertFileEnv   = "HOLOLIVE_H3_CERT_FILE"
	internalH3ServerNameEnv = "HOLOLIVE_INTERNAL_H3_SERVER_NAME"
	hololiveH3ServerNameEnv = "HOLOLIVE_H3_SERVER_NAME"
)

// NewJSONClient는 내부 서비스 URL scheme에 맞는 JSON client를 생성합니다.
func NewJSONClient(baseURL, apiKey string, timeout time.Duration, logger *slog.Logger) *httputil.JSONClient {
	return httputil.NewJSONClientWithHTTPClient(baseURL, apiKey, NewClientForURL(baseURL, timeout, logger))
}

// NewClientForURL은 https 내부 URL에는 H3 client를, 그 외에는 기존 internal HTTP client를 반환합니다.
func NewClientForURL(rawURL string, timeout time.Duration, logger *slog.Logger) *http.Client {
	if !internalURLUsesHTTPS(rawURL) {
		return httputil.NewInternalServiceClient(timeout)
	}

	client, err := newHTTP3Client(timeout)
	if err != nil {
		if logger != nil {
			logger.Warn("Failed to configure internal H3 client; falling back to default client", slog.Any("error", err))
		}
		return httputil.NewInternalServiceClient(timeout)
	}
	return client
}

func internalURLUsesHTTPS(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https"
}

func newHTTP3Client(timeout time.Duration) (*http.Client, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: firstNonEmptyEnv(internalH3ServerNameEnv, hololiveH3ServerNameEnv),
	}
	caFile := firstNonEmptyEnv(internalH3CACertFileEnv, hololiveH3CertFileEnv)
	if caFile != "" {
		roots, err := loadRootCAs(caFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = roots
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http3.Transport{
			TLSClientConfig: tlsConfig,
			QUICConfig: &quic.Config{
				InitialPacketSize: 1200,
			},
		},
	}, nil
}

func loadRootCAs(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read internal h3 CA file: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("read internal h3 CA file: no PEM certificates in %s", path)
	}
	return roots, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
