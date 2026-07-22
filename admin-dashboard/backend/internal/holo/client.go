package holo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/hololive-shared/pkg/httpbody"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
	"github.com/park285/shared-go/pkg/json"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type ProxyResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

func NewClient(baseURL, apiKey string) (*Client, error) {
	baseURL, err := normalizeHoloBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	httpClient, err := newHoloHTTPClient(baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		http:    httpClient,
	}, nil
}

func normalizeHoloBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid holo admin api url: %w", err)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if err := validateHoloBaseURL(parsed); err != nil {
		return "", err
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func validateHoloBaseURL(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid holo admin api url: scheme must be http or https")
	}
	if parsed.Hostname() == "" || parsed.Opaque != "" {
		return fmt.Errorf("invalid holo admin api url: host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("invalid holo admin api url: user info is not allowed")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("invalid holo admin api url: query and fragment are not allowed")
	}
	return nil
}

const holoClientTimeout = 10 * time.Second

func newHoloHTTPClient(baseURL string) (*http.Client, error) {
	return internalhttp.NewClientForURLStrict(baseURL, holoClientTimeout, nil)
}

const maxProxyBodyBytes = 8 << 20

func (c *Client) Proxy(ctx context.Context, method, path string, query url.Values, body []byte) (ProxyResponse, error) {
	req, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return ProxyResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ProxyResponse{}, proxyBadGateway(fmt.Errorf("request holo admin api: %w", err))
	}
	if resp == nil {
		return ProxyResponse{}, proxyBadGateway(fmt.Errorf("request holo admin api: empty response"))
	}
	return proxyResponseFromHTTP(resp)
}

func proxyResponseFromHTTP(resp *http.Response) (ProxyResponse, error) {
	if resp.StatusCode >= http.StatusInternalServerError {
		if err := httpbody.DrainAndClose(resp.Body, httpbody.DefaultDrainLimit); err != nil {
			return ProxyResponse{}, proxyBadGateway(fmt.Errorf("holo admin api returned status %d: drain response body: %w", resp.StatusCode, err))
		}
		return ProxyResponse{}, proxyBadGateway(fmt.Errorf("holo admin api returned status %d", resp.StatusCode))
	}

	respBody, err := httpbody.ReadAllAndClose(resp.Body, maxProxyBodyBytes)
	if err != nil {
		return ProxyResponse{}, proxyBadGateway(fmt.Errorf("read holo admin api response body: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ProxyResponse{}, upstreamError(resp.StatusCode, respBody)
	}
	return ProxyResponse{StatusCode: resp.StatusCode, Body: respBody, Header: resp.Header.Clone()}, nil
}

func proxyBadGateway(cause error) *httpx.AppError {
	err := httpx.BadGateway()
	err.Cause = cause
	return err
}

func (c *Client) buildRequest(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Request, error) {
	upstreamURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, proxyBadGateway(fmt.Errorf("build holo admin api url: %w", err))
	}
	if len(query) > 0 {
		upstreamURL.RawQuery = query.Encode()
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, upstreamURL.String(), reader)
	if err != nil {
		return nil, proxyBadGateway(fmt.Errorf("build holo admin api request: %w", err))
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	return req, nil
}

func upstreamError(status int, body []byte) error {
	fallback := http.StatusText(status)
	if fallback == "" {
		fallback = "The upstream service rejected the request"
	}
	if len(body) == 0 {
		return &httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: fallback}}
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return rawTextError(status, body, fallback)
	}
	return decodedUpstreamError(status, raw, fallback)
}

func rawTextError(status int, body []byte, fallback string) error {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = fallback
	}
	return &httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: text}}
}

func decodedUpstreamError(status int, raw any, fallback string) error {
	switch value := raw.(type) {
	case string:
		return &httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: value}}
	case map[string]any:
		return objectUpstreamError(status, value, fallback)
	default:
		return &httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: fallback, Details: raw}}
	}
}

func objectUpstreamError(status int, value map[string]any, fallback string) error {
	errorText, ok := value["error"].(string)
	if !ok {
		errorText = ""
	}
	if errorText == "" {
		errorText = fallback
	}
	code, ok := value["code"].(string)
	if !ok {
		code = ""
	}
	delete(value, "error")
	delete(value, "code")
	var details any
	if len(value) > 0 {
		details = value
	}
	return &httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: errorText, Code: code, Details: details}}
}
