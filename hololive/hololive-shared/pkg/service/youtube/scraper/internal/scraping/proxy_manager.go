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

package scraping

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/net/proxy"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type ProxyConfig struct {
	Enabled bool
	URL     string
}

func WithProxy(config ProxyConfig) ClientOption {
	return func(c *Client) {
		c.proxyConfig = config
	}
}

func (c *Client) initHTTPClients() {
	if c == nil {
		return
	}

	if c.httpClient != nil {
		c.activateInjectedHTTPClient()
		return
	}

	c.initDirectHTTPClient()
	c.initProxyHTTPClient()
	c.activateConfiguredHTTPClient()
}

func (c *Client) activateInjectedHTTPClient() {
	c.activeHTTPClient.Store(c.httpClient)
	c.proxyEnabled.Store(false)
}

func (c *Client) initDirectHTTPClient() {
	directClient, directTransport, err := createHTTPClient(ProxyConfig{})
	if err != nil {
		slog.Error("Failed to create direct scraper client, using fallback default transport",
			"error", err)
		directClient = &http.Client{Timeout: constants.YouTubeConfig.ScraperHTTPTimeout}
	}
	c.directHTTPClient = directClient
	c.directTransport = directTransport
}

func (c *Client) initProxyHTTPClient() {
	if c.proxyConfig.URL == "" {
		return
	}
	proxyClient, proxyTransport, err := createHTTPClient(ProxyConfig{Enabled: true, URL: c.proxyConfig.URL})
	if err != nil {
		slog.Error("Failed to create proxy scraper client, proxy toggle unavailable until restart",
			"error", err)
		return
	}
	c.proxyHTTPClient = proxyClient
	c.proxyTransport = proxyTransport
}

func (c *Client) activateConfiguredHTTPClient() {
	if c.proxyConfig.Enabled && c.proxyHTTPClient != nil {
		c.activeHTTPClient.Store(c.proxyHTTPClient)
		c.proxyEnabled.Store(true)
		return
	}

	c.activeHTTPClient.Store(c.directHTTPClient)
	c.proxyEnabled.Store(false)
	if c.proxyConfig.Enabled && c.proxyHTTPClient == nil {
		slog.Warn("Scraper proxy requested but unavailable, starting in direct mode")
	}
}

// proxy client가 준비되지 않았으면 true 요청은 적용되지 않고 direct 모드로 유지됩니다.
func (c *Client) SetProxyEnabled(enabled bool) bool {
	if c.httpClient != nil {
		// 외부 주입 클라이언트는 런타임 토글 대상이 아님
		return false
	}

	if enabled {
		if c.proxyHTTPClient == nil {
			c.proxyEnabled.Store(false)
			if c.directHTTPClient != nil {
				c.activeHTTPClient.Store(c.directHTTPClient)
			}
			return false
		}
		c.activeHTTPClient.Store(c.proxyHTTPClient)
		c.proxyEnabled.Store(true)
		c.proxyHealth.Arm()
		return true
	}

	if c.directHTTPClient == nil {
		return false
	}
	c.activeHTTPClient.Store(c.directHTTPClient)
	c.proxyEnabled.Store(false)
	return true
}

func (c *Client) ProxyEnabled() bool {
	return c.proxyEnabled.Load()
}

func (c *Client) currentHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	if active := c.activeHTTPClient.Load(); active != nil {
		return active
	}
	if c.directHTTPClient != nil {
		return c.directHTTPClient
	}
	return &http.Client{Timeout: constants.YouTubeConfig.ScraperHTTPTimeout}
}

func (c *Client) closeIdleConnections() {
	seen := closeIdleTransports([]*http.Transport{
		c.directTransport,
		c.proxyTransport,
	})
	c.closeIdleClientTransports(seen, []*http.Client{
		c.httpClient,
		c.directHTTPClient,
		c.proxyHTTPClient,
		c.activeHTTPClient.Load(),
	})
}

func closeIdleTransports(transports []*http.Transport) map[*http.Transport]struct{} {
	seen := make(map[*http.Transport]struct{})
	for _, transport := range transports {
		if transport == nil {
			continue
		}
		if _, exists := seen[transport]; exists {
			continue
		}
		seen[transport] = struct{}{}
		transport.CloseIdleConnections()
	}
	return seen
}

func (c *Client) closeIdleClientTransports(seen map[*http.Transport]struct{}, clients []*http.Client) {
	for _, client := range clients {
		closeIdleClientTransport(seen, client)
	}
}

func closeIdleClientTransport(seen map[*http.Transport]struct{}, client *http.Client) {
	if client == nil {
		return
	}
	transport, ok := client.Transport.(interface{ CloseIdleConnections() })
	if !ok || transport == nil {
		return
	}
	httpTransport, ok := unwrapHTTPTransport(client.Transport)
	if !ok || httpTransport == nil {
		transport.CloseIdleConnections()
		return
	}
	if _, exists := seen[httpTransport]; exists {
		return
	}
	seen[httpTransport] = struct{}{}
	transport.CloseIdleConnections()
}

// createHTTPClient: 프록시 설정에 따라 HTTP 클라이언트 생성
func createHTTPClient(proxyConfig ProxyConfig) (*http.Client, *http.Transport, error) {
	baseTransport := newScraperTransport(true)

	if !proxyConfig.Enabled || proxyConfig.URL == "" {
		slog.Info("Scraper using direct connection (no proxy)")
		baseTransport.DialContext = newDirectDialContext()
		return &http.Client{
			Transport: instrumentScraperTransport(baseTransport),
			Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
		}, baseTransport, nil
	}

	parsedURL, err := url.Parse(proxyConfig.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if err := validateSOCKS5ProxyURL(parsedURL); err != nil {
		return nil, nil, err
	}

	auth := newProxyAuth(parsedURL)

	forwardDialer := &net.Dialer{
		Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
		KeepAlive: 30 * time.Second,
	}

	dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, forwardDialer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	transport := newScraperTransport(false)
	transport.DialContext = newSOCKS5DialContext(dialer)

	// #nosec G706 -- proxy host is loaded from trusted runtime configuration.
	slog.Info("Scraper using SOCKS5 proxy",
		"host", parsedURL.Host,
		"has_auth", auth != nil)

	return &http.Client{
		Transport: instrumentScraperTransport(transport),
		Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
	}, transport, nil
}

func validateSOCKS5ProxyURL(parsedURL *url.URL) error {
	if parsedURL == nil {
		return fmt.Errorf("proxy URL is nil")
	}
	if parsedURL.Scheme != "socks5" && parsedURL.Scheme != "socks5h" {
		return fmt.Errorf("unsupported proxy scheme: %s", parsedURL.Scheme)
	}
	if strings.TrimSpace(parsedURL.Host) == "" {
		return fmt.Errorf("proxy URL missing host")
	}
	host, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		return fmt.Errorf("proxy URL host must include port: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("proxy URL missing host")
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("proxy URL missing port")
	}
	return nil
}

func newScraperTransport(forceHTTP2 bool) *http.Transport {
	return &http.Transport{
		ForceAttemptHTTP2:     forceHTTP2,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   constants.YouTubeConfig.ScraperDialTimeout,
		ResponseHeaderTimeout: constants.YouTubeConfig.ScraperHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
}

func newDirectDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	return dialer.DialContext
}

func newProxyAuth(parsedURL *url.URL) *proxy.Auth {
	if parsedURL == nil || parsedURL.User == nil {
		return nil
	}

	password, _ := parsedURL.User.Password()
	return &proxy.Auth{
		User:     parsedURL.User.Username(),
		Password: password,
	}
}

func newSOCKS5DialContext(dialer proxy.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := contextDialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, fmt.Errorf("proxy dial failed: %w", err)
			}
			if ctx.Err() != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
			}
			return conn, nil
		}
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialSOCKS5WithContextFallback(ctx, dialer, network, addr)
	}
}

func instrumentScraperTransport(baseTransport *http.Transport) http.RoundTripper {
	if baseTransport == nil {
		return http.DefaultTransport
	}

	return otelhttp.NewTransport(baseTransport)
}

func unwrapHTTPTransport(roundTripper http.RoundTripper) (*http.Transport, bool) {
	transport, ok := roundTripper.(*http.Transport)
	if ok {
		return transport, true
	}
	return nil, false
}
