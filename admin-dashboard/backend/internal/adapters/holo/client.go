package holo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient는 고정 upstream과 전용 HTTP client를 만들며 redirect·인증 key의 다른 목적지 전달을 거부합니다.
func NewClient(baseURL, apiKey string) (*Client, error) {
	baseURL, err := normalizeHoloBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("normalize holo base URL: %w", err)
	}

	httpClient, err := newHoloHTTPClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("holo HTTP client: %w", err)
	}

	// 고정 upstream의 redirect는 허용하지 않아 API key 전달·자동 후속 요청을 막습니다.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

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
		return "", fmt.Errorf("validate holo base URL: %w", err)
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

func validateHoloBaseURL(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("invalid holo admin api url: scheme must be http or https")
	}

	if parsed.Hostname() == "" || parsed.Opaque != "" {
		return errors.New("invalid holo admin api url: host is required")
	}

	if parsed.User != nil {
		return errors.New("invalid holo admin api url: user info is not allowed")
	}

	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return errors.New("invalid holo admin api url: query and fragment are not allowed")
	}

	return nil
}

const holoClientTimeout = 10 * time.Second

func newHoloHTTPClient(baseURL string) (*http.Client, error) {
	out, err := internalhttp.NewClientForURLStrict(baseURL, holoClientTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("client for URL strict: %w", err)
	}

	return out, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	if err := internalhttp.CloseClient(c.http); err != nil {
		return fmt.Errorf("close client: %w", err)
	}

	return nil
}

const maxProxyBodyBytes = 8 << 20

const responseBodyDrainLimit int64 = 64 << 10

var errInvalidOwnedResponse = errors.New("upstream response violates the owned contract")

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body []byte, status int, target any) error {
	req, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if method != http.MethodGet {
		contract.MarkDispatched(ctx)
	}

	resp, err := c.http.Do(req) //nolint:bodyclose // decodeHTTPResponse가 response body를 모든 반환 경로에서 닫는다.
	if err != nil {
		return proxyBadGateway(fmt.Errorf("request holo admin api: %w", err))
	}

	if resp == nil {
		return proxyBadGateway(errors.New("request holo admin api: empty response"))
	}

	if err := decodeHTTPResponse(resp, status, target); err != nil {
		return fmt.Errorf("decode upstream HTTP response: %w", err)
	}

	return nil
}

func decodeHTTPResponse(resp *http.Response, status int, target any) error {
	if resp.StatusCode >= http.StatusInternalServerError {
		if err := httputil.DrainAndClose(resp.Body, responseBodyDrainLimit); err != nil {
			return proxyBadGateway(fmt.Errorf("holo admin api returned status %d: drain response body: %w", resp.StatusCode, err))
		}

		return proxyBadGateway(fmt.Errorf("holo admin api returned status %d", resp.StatusCode))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, err := httputil.ReadAllAndCloseWithDrainLimit(resp.Body, maxProxyBodyBytes, responseBodyDrainLimit)
		if err != nil {
			return proxyBadGateway(fmt.Errorf("read holo admin api error response: %w", err))
		}

		return fmt.Errorf("upstream error: %w", upstreamError(resp.StatusCode, respBody))
	}

	if err := decodeOwnedBody(resp, status, target); err != nil {
		return proxyBadGateway(fmt.Errorf("decode holo admin api body: %w", err))
	}

	return nil
}

// ownedResponseReader는 decoder가 소비한 첫 I/O 실패를 보존합니다. Go 1.27의 custom decoder
// 진입 전 EOF 검사와 n>0인 읽기에서 오류가 유실될 수 있으므로 JSON 성공 여부와 별도로 확인합니다.
type ownedResponseReader struct {
	reader io.Reader
	err    error
}

func (r *ownedResponseReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF && r.err == nil {
		r.err = err
	}

	return n, err
}

func decodeOwnedBody(resp *http.Response, status int, target any) (err error) {
	if resp.Body == nil {
		return httputil.ErrNilBody
	}

	defer func() {
		var closeErr error

		if err != nil {
			closeErr = httputil.DrainAndClose(resp.Body, responseBodyDrainLimit)
		} else {
			closeErr = resp.Body.Close()
		}

		err = errors.Join(err, closeErr)
	}()

	mediaType, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" || resp.StatusCode != status {
		return errInvalidOwnedResponse
	}

	// 전체 body의 중간 복사 없이 DTO로 읽되 마지막 공백·추가 JSON까지 같은 8 MiB 상한에 포함합니다.
	observed := ownedResponseReader{reader: resp.Body}
	limited := io.LimitedReader{R: &observed, N: maxProxyBodyBytes + 1}
	// JSON decoder의 작은 읽기를 모으되 선행 읽기도 같은 전체 body 상한 안에 둡니다.
	buffered := bufio.NewReaderSize(&limited, 32<<10)
	decodeErr := jsonv2.UnmarshalRead(buffered, target)

	if limited.N == 0 {
		return httputil.ErrResponseBodyTooLarge
	}

	if observed.err != nil {
		return observed.err
	}

	// JSON 오류에는 필드 값이 포함될 수 있으므로 원문을 보존하지 않습니다. I/O 원인은 그대로 전달합니다.
	if _, ok := errors.AsType[*jsonv2.SemanticError](decodeErr); ok {
		return errInvalidOwnedResponse
	}

	if _, ok := errors.AsType[*jsontext.SyntacticError](decodeErr); ok {
		return errInvalidOwnedResponse
	}

	return decodeErr
}

func proxyBadGateway(cause error) *contract.AppError {
	err := contract.BadGateway()

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

func upstreamError(status int, _ []byte) *contract.AppError {
	if status == http.StatusUnauthorized {
		return &contract.AppError{Status: http.StatusBadGateway, Body: contract.ErrorResponse{Code: "UPSTREAM_AUTH_FAILED", Error: "Internal service authentication failed"}}
	}

	switch status {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
		return &contract.AppError{Status: status, Body: contract.ErrorResponse{Code: "UPSTREAM_REJECTED", Error: "The upstream service rejected the request"}}
	default:
		return &contract.AppError{Status: http.StatusBadGateway, Body: contract.ErrorResponse{Code: "UPSTREAM_RESPONSE_INVALID", Error: "Unexpected upstream response"}}
	}
}
