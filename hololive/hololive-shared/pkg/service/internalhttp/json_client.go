package internalhttp

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	sharedh3 "github.com/park285/shared-go/pkg/h3"
	"github.com/park285/shared-go/pkg/httputil"
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

	client, _, err := sharedh3.NewClient(timeout, sharedh3.ClientOptions{
		CACertFile: firstNonEmptyEnv(internalH3CACertFileEnv, hololiveH3CertFileEnv),
		ServerName: firstNonEmptyEnv(internalH3ServerNameEnv, hololiveH3ServerNameEnv),
	})
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

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
