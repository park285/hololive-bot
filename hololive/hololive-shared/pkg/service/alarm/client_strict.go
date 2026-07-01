package alarm

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

// 범용 internal client가 쓰는 transport fallback 없이 alarm service client를 만든다.
// big-bang runtime 조립이 이 생성자를 쓰는 이유는, CA 누락·잘못된 server name·손상된
// H3 transport가 bot/admin listener가 트래픽을 받기 전에 실패하도록 하기 위해서다.
func NewClientWithAPIKeyStrict(baseURL, apiKey string, logger *slog.Logger) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("alarm service base URL is required")
	}
	if err := validateAlarmServiceOrigin(baseURL); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	httpClient, err := internalhttp.NewClientForURLStrict(baseURL, 10*time.Second, logger)
	if err != nil {
		return nil, fmt.Errorf("configure alarm service transport: %w", err)
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
		logger:     logger,
	}, nil
}

func validateAlarmServiceOrigin(baseURL string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parse alarm service base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("alarm service URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("alarm service URL must include a host")
	}
	if alarmOriginHasDisallowedParts(baseURL, parsed) {
		return fmt.Errorf("alarm service URL must be an origin without credentials, path, query or fragment")
	}
	return nil
}

func alarmOriginHasDisallowedParts(raw string, parsed *url.URL) bool {
	if parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || strings.Contains(raw, "#") {
		return true
	}
	return parsed.Path != "" && parsed.Path != "/"
}
