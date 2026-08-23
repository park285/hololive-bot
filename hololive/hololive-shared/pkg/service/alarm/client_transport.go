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

func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) putJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	bodyReader, err := encodeJSONRequestBody(path, body)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return err
	}
	defer c.closeResponseBody(resp, path)

	if err := decodeAPIEnvelope(path, resp.Body, out); err != nil {
		return err
	}
	return nil
}

func decodeAPIEnvelope(path string, body io.Reader, out any) error {
	var envelope apiEnvelope
	if err := jsonv2.UnmarshalRead(body, &envelope); err != nil {
		return fmt.Errorf("alarm-api: %s: decode envelope: %w", path, err)
	}
	if !envelope.Success {
		return fmt.Errorf("alarm-api: %s: %s", path, envelopeFailureMessage(envelope))
	}
	if !envelopeHasData(out, envelope.Data) {
		return nil
	}
	if err := jsonv2.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("alarm-api: %s: decode data: %w", path, err)
	}
	return nil
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

func envelopeHasData(out any, data jsontext.Value) bool {
	if out == nil || len(data) == 0 {
		return false
	}
	return !bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

func encodeJSONRequestBody(path string, body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := jsonv2.MarshalWrite(&buf, body); err != nil {
		return nil, fmt.Errorf("alarm-api: %s: encode request: %w", path, err)
	}
	return &buf, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, body, hasJSONBody)
	if err != nil {
		return nil, err
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
		return nil, err
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
