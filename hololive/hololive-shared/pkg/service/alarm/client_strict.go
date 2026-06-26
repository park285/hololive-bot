package alarm

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

// NewClientWithAPIKeyStrict constructs the alarm service client without the
// transport fallback used by general-purpose internal clients. Big-bang runtime
// assembly uses this constructor so a missing CA, invalid server name or broken
// H3 transport fails before the bot/admin listeners accept traffic.
func NewClientWithAPIKeyStrict(baseURL, apiKey string, logger *slog.Logger) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("alarm service base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse alarm service base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("alarm service URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("alarm service URL must include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("alarm service URL must be an origin without credentials, path, query or fragment")
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
