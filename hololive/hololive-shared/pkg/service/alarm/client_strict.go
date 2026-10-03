package alarm

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

// 범용 internal client가 쓰는 transport fallback 없이 alarm service client를 만든다.
// 이 생성자를 big-bang runtime 조립이 쓰는 이유는, CA 누락·잘못된 server name·손상된
// H3 transport가 bot/admin listener가 트래픽을 받기 전에 실패하도록 하기 위해서다.
func NewClientWithAPIKeyStrict(baseURL, apiKey string, logger *slog.Logger, options sharedh3.ClientOptions) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("alarm service base URL is required")
	}

	if err := validateAlarmServiceOrigin(baseURL); err != nil {
		return nil, fmt.Errorf("validate alarm service origin: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	httpClient, err := internalhttp.NewClientForURLStrict(baseURL, 10*time.Second, options)
	if err != nil {
		return nil, fmt.Errorf("configure alarm service transport: %w", err)
	}

	return &Client{
		baseURL:         baseURL,
		apiKey:          strings.TrimSpace(apiKey),
		httpClient:      httpClient,
		logger:          logger,
		advanceRequests: make(chan struct{}, 1),
	}, nil
}

// Close는 alarm-worker로 가는 H3 transport의 연결을 닫는다. 소유 plane이 요청을 모두 끝낸 뒤 부른다.
// 닫지 않으면 worker의 graceful shutdown이 이 연결을 QUIC idle timeout까지 기다린다.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	if err := internalhttp.CloseClient(c.httpClient); err != nil {
		return fmt.Errorf("close alarm service client: %w", err)
	}

	return nil
}

func validateAlarmServiceOrigin(baseURL string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parse alarm service base URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("alarm service URL scheme must be http or https")
	}

	if parsed.Host == "" {
		return errors.New("alarm service URL must include a host")
	}

	if alarmOriginHasDisallowedParts(baseURL, parsed) {
		return errors.New("alarm service URL must be an origin without credentials, path, query or fragment")
	}

	return nil
}

func alarmOriginHasDisallowedParts(raw string, parsed *url.URL) bool {
	if parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || strings.Contains(raw, "#") {
		return true
	}

	return parsed.Path != "" && parsed.Path != "/"
}
