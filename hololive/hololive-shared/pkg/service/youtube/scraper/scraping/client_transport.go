package scraping

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// initHTTPClients는 주입된 client가 없을 때 직접 연결 client 하나를 만든다. SOCKS5 proxy client와 런타임 proxy 토글은
// DEC-20260926-hololive-legacy-env-config-retirement로 지웠으므로 연결 경로는 이것 하나다.
func (c *Client) initHTTPClients() {
	if c == nil || c.httpClient != nil {
		return
	}

	c.httpClient, c.transport = newDirectHTTPClient(&c.config)
}

func (c *Client) currentHTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) closeIdleConnections() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()

		return
	}

	if c.httpClient == nil {
		return
	}

	if transport, ok := c.httpClient.Transport.(interface{ CloseIdleConnections() }); ok && transport != nil {
		transport.CloseIdleConnections()
	}
}

// newDirectHTTPClient는 runtime YouTube 설정의 timeout으로 HTTP/1 전용 직접 연결 client를 만든다.
func newDirectHTTPClient(config *settings.YouTubeConfig) (*http.Client, *http.Transport) {
	slog.Info("Scraper using direct connection")

	transport := newScraperTransport(config)

	transport.DialContext = newDirectDialContext(config.ScraperDialTimeout)

	return &http.Client{
		Transport: transport,
		Timeout:   config.ScraperHTTPTimeout,
	}, transport
}

func newScraperTransport(config *settings.YouTubeConfig) *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)

	return &http.Transport{
		Protocols:             protocols,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   config.ScraperDialTimeout,
		ResponseHeaderTimeout: config.ScraperHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
}

func newDirectDialContext(dialTimeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}

	return dialer.DialContext
}
