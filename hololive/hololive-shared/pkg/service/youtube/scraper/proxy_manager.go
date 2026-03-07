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

package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// ProxyConfig: 프록시 설정
type ProxyConfig struct {
	Enabled bool   // 프록시 사용 여부
	URL     string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
}

// WithProxy: SOCKS5 프록시 설정
func WithProxy(cfg ProxyConfig) ClientOption {
	return func(c *Client) {
		c.proxyConfig = cfg
	}
}

func (c *Client) initHTTPClients() {
	if c == nil {
		return
	}

	if c.httpClient != nil {
		c.activeHTTPClient.Store(c.httpClient)
		c.proxyEnabled.Store(false)
		return
	}

	directClient, err := createHTTPClient(ProxyConfig{})
	if err != nil {
		slog.Error("Failed to create direct scraper client, using fallback default transport",
			"error", err)
		directClient = &http.Client{Timeout: constants.YouTubeConfig.ScraperHTTPTimeout}
	}
	c.directHTTPClient = directClient

	if c.proxyConfig.URL != "" {
		proxyClient, proxyErr := createHTTPClient(ProxyConfig{Enabled: true, URL: c.proxyConfig.URL})
		if proxyErr != nil {
			slog.Error("Failed to create proxy scraper client, proxy toggle unavailable until restart",
				"error", proxyErr)
		} else {
			c.proxyHTTPClient = proxyClient
		}
	}

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

// SetProxyEnabled: 런타임에 프록시 사용 여부를 토글합니다.
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
		return true
	}

	if c.directHTTPClient == nil {
		return false
	}
	c.activeHTTPClient.Store(c.directHTTPClient)
	c.proxyEnabled.Store(false)
	return true
}

// ProxyEnabled: 현재 런타임 프록시 활성 상태를 반환합니다.
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
	clients := []*http.Client{
		c.httpClient,
		c.directHTTPClient,
		c.proxyHTTPClient,
		c.activeHTTPClient.Load(),
	}
	seen := make(map[*http.Transport]struct{})
	for _, client := range clients {
		if client == nil {
			continue
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport == nil {
			continue
		}
		if _, exists := seen[transport]; exists {
			continue
		}
		seen[transport] = struct{}{}
		transport.CloseIdleConnections()
	}
}

// createHTTPClient: 프록시 설정에 따라 HTTP 클라이언트 생성
func createHTTPClient(proxyCfg ProxyConfig) (*http.Client, error) {
	baseTransport := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   constants.YouTubeConfig.ScraperDialTimeout,
		ResponseHeaderTimeout: constants.YouTubeConfig.ScraperHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if !proxyCfg.Enabled || proxyCfg.URL == "" {
		slog.Info("Scraper using direct connection (no proxy)")
		dialer := &net.Dialer{
			Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
			KeepAlive: 30 * time.Second,
		}
		baseTransport.DialContext = dialer.DialContext
		return &http.Client{
			Transport: baseTransport,
			Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
		}, nil
	}

	parsedURL, err := url.Parse(proxyCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	var auth *proxy.Auth
	if parsedURL.User != nil {
		password, _ := parsedURL.User.Password()
		auth = &proxy.Auth{
			User:     parsedURL.User.Username(),
			Password: password,
		}
	}

	forwardDialer := &net.Dialer{
		Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
		KeepAlive: 30 * time.Second,
	}

	dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, forwardDialer)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          baseTransport.MaxIdleConns,
		MaxIdleConnsPerHost:   baseTransport.MaxIdleConnsPerHost,
		IdleConnTimeout:       baseTransport.IdleConnTimeout,
		TLSHandshakeTimeout:   baseTransport.TLSHandshakeTimeout,
		ResponseHeaderTimeout: baseTransport.ResponseHeaderTimeout,
		ExpectContinueTimeout: baseTransport.ExpectContinueTimeout,
	}

	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
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
	} else {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialSOCKS5WithContextFallback(ctx, dialer, network, addr)
		}
	}

	// #nosec G706 -- proxy host is loaded from trusted runtime configuration.
	slog.Info("Scraper using SOCKS5 proxy",
		"host", parsedURL.Host,
		"has_auth", auth != nil)

	return &http.Client{
		Transport: transport,
		Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
	}, nil
}

type dialResult struct {
	conn net.Conn
	err  error
}

func dialSOCKS5WithContextFallback(ctx context.Context, dialer proxy.Dialer, network, addr string) (net.Conn, error) {
	done := make(chan dialResult, 1)

	go func() {
		conn, err := dialer.Dial(network, addr)
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}

		select {
		case done <- dialResult{conn: conn, err: err}:
		default:
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		select {
		case result := <-done:
			if result.conn != nil {
				_ = result.conn.Close()
			}
		default:
		}
		return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
	case result := <-done:
		if result.err != nil {
			return nil, fmt.Errorf("proxy dial failed: %w", result.err)
		}
		if ctx.Err() != nil {
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
		}
		return result.conn, nil
	}
}
