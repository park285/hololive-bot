package alarm

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

func (c *Client) postJSON[T any](ctx context.Context, path string, body any) (T, error) {
	return c.doJSON[T](ctx, http.MethodPost, path, body)
}

func (c *Client) getJSON[T any](ctx context.Context, path string) (T, error) {
	return c.doJSON[T](ctx, http.MethodGet, path, nil)
}

func (c *Client) putJSON[T any](ctx context.Context, path string, body any) (T, error) {
	return c.doJSON[T](ctx, http.MethodPut, path, body)
}

func (c *Client) doJSON[T any](ctx context.Context, method, path string, body any) (T, error) {
	var zero T

	bodyReader, err := encodeJSONRequestBody(path, body)
	if err != nil {
		return zero, fmt.Errorf("encode JSON request body: %w", err)
	}

	resp, err := c.doRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return zero, fmt.Errorf("do request: %w", err)
	}
	defer c.closeResponseBody(resp, path)

	return decodeAPIEnvelope[T](path, resp.Body)
}

func (c *Client) putNoData(ctx context.Context, path string, body any) error {
	bodyReader, err := encodeJSONRequestBody(path, body)
	if err != nil {
		return fmt.Errorf("encode JSON request body: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, path, bodyReader, body != nil)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer c.closeResponseBody(resp, path)

	if _, envErr := readAPIEnvelope(path, resp.Body); envErr != nil {
		return fmt.Errorf("read API envelope: %w", envErr)
	}

	return nil
}

func readAPIEnvelope(path string, body io.Reader) (apiEnvelope, error) {
	var envelope apiEnvelope

	if err := jsonv2.UnmarshalRead(body, &envelope); err != nil {
		return apiEnvelope{}, fmt.Errorf("alarm-api: %s: decode envelope: %w", path, err)
	}

	if !envelope.Success {
		return apiEnvelope{}, fmt.Errorf("alarm-api: %s: %s", path, envelopeFailureMessage(envelope))
	}

	return envelope, nil
}

func decodeAPIEnvelope[T any](path string, body io.Reader) (T, error) {
	var out T

	envelope, err := readAPIEnvelope(path, body)
	if err != nil {
		return out, fmt.Errorf("read API envelope: %w", err)
	}

	if !envelopeHasData(envelope.Data) {
		return out, nil
	}

	if err := jsonv2.Unmarshal(envelope.Data, &out); err != nil {
		var zero T

		return zero, fmt.Errorf("alarm-api: %s: decode data: %w", path, err)
	}

	return out, nil
}

func envelopeFailureMessage(envelope apiEnvelope) string {
	message := strings.TrimSpace(envelope.Message)
	if message == "" {
		message = strings.TrimSpace(envelope.Error)
	}

	if message == "" {
		message = "provider returned unsuccessful response"
	}

	return message
}

func envelopeHasData(data jsontext.Value) bool {
	if len(data) == 0 {
		return false
	}

	return !bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

func encodeJSONRequestBody(path string, body any) (io.Reader, error) {
	var buf bytes.Buffer

	if body == nil {
		// nil reader는 "본문 없음"이라 http.NoBody로 바꾸면 Content-Length가 0으로 붙는다.
		var empty io.Reader

		return empty, nil
	}

	if err := jsonv2.MarshalWrite(&buf, body); err != nil {
		return nil, fmt.Errorf("alarm-api: %s: encode request: %w", path, err)
	}

	return &buf, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, body, hasJSONBody)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if resp == nil {
			err = fmt.Errorf("nil response: %w", err)
		}

		return nil, fmt.Errorf("alarm-api: %s: %w", path, err)
	}

	if resp == nil {
		return nil, fmt.Errorf("alarm-api: %s: nil response", path)
	}

	if err := c.validateResponse(path, resp); err != nil {
		return nil, fmt.Errorf("validate response: %w", err)
	}

	return resp, nil
}

func (c *Client) validateResponse(path string, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("alarm-api: %s: nil response", path)
	}

	if resp.Body == nil {
		return fmt.Errorf("alarm-api: %s: nil response body", path)
	}

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("alarm-api: %s: check status: %w", path, err)
	}

	return nil
}

func (c *Client) closeResponseBody(resp *http.Response, path string) {
	if resp == nil || resp.Body == nil {
		return
	}

	if err := resp.Body.Close(); err != nil {
		c.logger.Warn("Failed to close alarm API response body", slog.String("path", path), slog.Any("error", err))
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("alarm-api: %s: new request: %w", path, err)
	}

	if hasJSONBody {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	return req, nil
}
