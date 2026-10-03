package botrooms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type Client struct {
	httpClient *internalhttp.JSONClient
}

// Close는 bot plane 내부 API로 가는 H3 transport의 연결을 닫는다. 관리 plane이 요청을 모두 끝낸 뒤 부른다.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	if err := c.httpClient.Close(); err != nil {
		return fmt.Errorf("close bot rooms client: %w", err)
	}

	return nil
}

func NewClient(baseURL, apiKey string, options sharedh3.ClientOptions) (*Client, error) {
	validatedBaseURL, err := validateInternalBotRoomsBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("validate internal bot rooms base URL: %w", err)
	}

	httpClient, err := internalhttp.NewJSONClient(validatedBaseURL, apiKey, 30*time.Second, options)
	if err != nil {
		return nil, fmt.Errorf("configure internal bot rooms transport: %w", err)
	}

	return &Client{httpClient: httpClient}, nil
}

func (c *Client) GetRooms(ctx context.Context) (*iris.RoomListResponse, error) {
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	req, err := c.httpClient.NewRequest(ctx, http.MethodGet, irisroomscontracts.ListPath)
	if err != nil {
		return nil, fmt.Errorf("list bot iris rooms: new request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // CheckStatus/DecodeJSON이 성공·실패 경로에서 body를 닫는다.
	if err != nil {
		return nil, fmt.Errorf("list bot iris rooms: request: %w", err)
	}

	if resp == nil {
		return nil, errors.New("list bot iris rooms: response is nil")
	}

	if resp.Body == nil {
		return nil, errors.New("list bot iris rooms: response body is nil")
	}

	if checkErr := c.httpClient.CheckStatus(resp); checkErr != nil {
		return nil, fmt.Errorf("list bot iris rooms: request failed: %w", checkErr)
	}

	out, err := c.httpClient.DecodeJSON[iris.RoomListResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("list bot iris rooms: decode response: %w", err)
	}

	return &out, nil
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("list bot iris rooms: client is nil")
	}

	if c.httpClient == nil {
		return errors.New("list bot iris rooms: http client is not configured")
	}

	return nil
}

var allowedInternalBotRoomsHosts = map[string]struct{}{
	"localhost":    {},
	"hololive-api": {},
	"bot.internal": {},
}

func validateInternalBotRoomsBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid bot internal URL: %w", err)
	}

	if err := validateInternalBotRoomsURLShape(parsed); err != nil {
		return "", fmt.Errorf("validate internal bot rooms URL shape: %w", err)
	}

	if !allowedInternalBotRoomsHost(parsed.Hostname()) {
		return "", fmt.Errorf("invalid bot internal URL host %q", parsed.Hostname())
	}

	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func validateInternalBotRoomsURLShape(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid bot internal URL scheme %q", parsed.Scheme)
	}

	if parsed.User != nil {
		return errors.New("invalid bot internal URL: credentials are not allowed")
	}

	if parsed.Hostname() == "" {
		return errors.New("invalid bot internal URL: host is required")
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("invalid bot internal URL: path must be empty")
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid bot internal URL: query and fragment are not allowed")
	}

	return nil
}

func allowedInternalBotRoomsHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback()
	}

	_, ok := allowedInternalBotRoomsHosts[host]

	return ok
}
