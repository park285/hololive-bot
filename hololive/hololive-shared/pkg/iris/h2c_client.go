// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package iris

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type H2CClient struct {
	baseURL  string
	botToken string // X-Bot-Token for outbound auth
	client   *http.Client
	logger   *slog.Logger
}

const (
	defaultHTTPTimeout            = 10 * time.Second
	defaultHTTPDialTimeout        = 3 * time.Second
	defaultHTTPTLSHandshake       = 5 * time.Second
	defaultHTTPResponseHeaderWait = 5 * time.Second
	defaultHTTPIdleConnTimeout    = 90 * time.Second
)

type H2CClientOptions struct {
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
}

func (o H2CClientOptions) normalized() H2CClientOptions {
	result := o
	if result.Timeout <= 0 {
		result.Timeout = defaultHTTPTimeout
	}
	if result.DialTimeout <= 0 {
		result.DialTimeout = defaultHTTPDialTimeout
	}
	if result.TLSHandshakeTimeout <= 0 {
		result.TLSHandshakeTimeout = defaultHTTPTLSHandshake
	}
	if result.ResponseHeaderTimeout <= 0 {
		result.ResponseHeaderTimeout = defaultHTTPResponseHeaderWait
	}
	if result.IdleConnTimeout <= 0 {
		result.IdleConnTimeout = defaultHTTPIdleConnTimeout
	}
	return result
}

func NewH2CClient(baseURL, botToken string, logger *slog.Logger, options ...H2CClientOptions) *H2CClient {
	baseURL = strings.TrimRight(baseURL, "/")
	if logger == nil {
		logger = slog.Default()
	}

	opt := H2CClientOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	opt = opt.normalized()

	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     opt.IdleConnTimeout,
		DialContext: (&net.Dialer{
			Timeout:   opt.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   opt.TLSHandshakeTimeout,
		ResponseHeaderTimeout: opt.ResponseHeaderTimeout,
	}

	client := httputil.NewClient(opt.Timeout)
	client.Transport = transport

	return &H2CClient{
		baseURL:  baseURL,
		botToken: botToken,
		client:   client,
		logger:   logger,
	}
}

func (c *H2CClient) SendMessage(ctx context.Context, room, message string, opts ...SendOption) error {
	o := applySendOptions(opts)
	reqBody := ReplyRequest{
		Type:     "text",
		Room:     room,
		Data:     message,
		ThreadID: o.ThreadID,
	}
	return c.postJSON(ctx, sharedirisx.PathReply, reqBody, nil)
}

func (c *H2CClient) SendImage(ctx context.Context, room, imageBase64 string) error {
	reqBody := ReplyRequest{
		Type: "image",
		Room: room,
		Data: imageBase64,
	}
	return c.postJSON(ctx, sharedirisx.PathReply, reqBody, nil)
}

func (c *H2CClient) Ping(ctx context.Context) bool {
	req, err := c.newRequest(ctx, http.MethodGet, "/config", nil)
	if err != nil {
		return false
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (c *H2CClient) GetConfig(ctx context.Context) (*Config, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/config", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get /config: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return nil, fmt.Errorf("get /config: %w", err)
	}

	var cfg Config
	if err := httputil.DecodeJSON(resp, &cfg); err != nil {
		return nil, fmt.Errorf("decode /config response: %w", err)
	}
	return &cfg, nil
}

func (c *H2CClient) Decrypt(ctx context.Context, data string) (string, error) {
	reqBody := DecryptRequest{
		B64Ciphertext: data,
		Enc:           0,
	}

	var respBody DecryptResponse
	if err := c.postJSON(ctx, "/decrypt", reqBody, &respBody); err != nil {
		return "", err
	}
	return respBody.PlainText, nil
}

func (c *H2CClient) postJSON(ctx context.Context, path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}

	if out == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("drain %s response body: %w", path, err)
		}
		return nil
	}

	if err := httputil.DecodeJSON(resp, out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}

func (c *H2CClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("new request %s %s: %w", method, path, err)
	}

	if strings.TrimSpace(c.botToken) != "" {
		req.Header.Set(sharedirisx.HeaderBotToken, c.botToken)
	}

	return req, nil
}
