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
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid holo admin api url: %w", err)
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

func newHoloHTTPClient(baseURL string) (*http.Client, error) {
	if strings.HasPrefix(strings.ToLower(baseURL), "https://") {
		return internalhttp.NewClientForURLStrict(baseURL, 10*time.Second, nil)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

const maxProxyBodyBytes = 8 << 20

func (c *Client) Proxy(ctx context.Context, method, path string, query url.Values, body []byte) (ProxyResponse, error) {
	req, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return ProxyResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ProxyResponse{}, httpx.BadGateway()
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxProxyBodyBytes+1))
	if err != nil {
		return ProxyResponse{}, httpx.BadGateway()
	}
	if len(respBody) > maxProxyBodyBytes {
		return ProxyResponse{}, httpx.BadGateway()
	}
	if resp.StatusCode >= 500 {
		return ProxyResponse{}, httpx.BadGateway()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProxyResponse{}, upstreamError(resp.StatusCode, respBody)
	}
	return ProxyResponse{StatusCode: resp.StatusCode, Body: respBody, Header: resp.Header.Clone()}, nil
}

func (c *Client) buildRequest(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Request, error) {
	upstreamURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, httpx.BadGateway()
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
		return nil, httpx.BadGateway()
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
		return httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: fallback}}
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
	return httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: text}}
}

func decodedUpstreamError(status int, raw any, fallback string) error {
	switch value := raw.(type) {
	case string:
		return httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: value}}
	case map[string]any:
		return objectUpstreamError(status, value, fallback)
	default:
		return httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: fallback, Details: raw}}
	}
}

func objectUpstreamError(status int, value map[string]any, fallback string) error {
	errorText, _ := value["error"].(string)
	if errorText == "" {
		errorText = fallback
	}
	code, _ := value["code"].(string)
	delete(value, "error")
	delete(value, "code")
	var details any
	if len(value) > 0 {
		details = value
	}
	return httpx.AppError{Status: status, Body: httpx.ErrorResponse{Error: errorText, Code: code, Details: details}}
}
