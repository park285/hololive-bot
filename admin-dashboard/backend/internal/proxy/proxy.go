// Package proxy: 도메인별 봇으로 요청 프록시
package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/net/http2"
)

// BotProxies: 각 봇에 대한 리버스 프록시
// 일반 API는 H2C, WebSocket은 HTTP/1.1 Transport를 사용한다.
type BotProxies struct {
	Holo       *httputil.ReverseProxy
	HoloWS     *httputil.ReverseProxy // WebSocket 전용 프록시
	holoAPIKey string                 // hololive-bot X-API-Key 주입용
	logger     *slog.Logger
}

// NewBotProxies: 봇 프록시 생성
func NewBotProxies(holoURL, holoAPIKey string, logger *slog.Logger) (*BotProxies, error) {
	proxyLogger := logger.With(slog.String("component", "proxy"))

	// H2C 프록시 (일반 API)
	holoProxy, err := createProxy(holoURL, proxyLogger, "holo")
	if err != nil {
		return nil, err
	}

	// HTTP/1.1 프록시 (WebSocket)
	holoWSProxy, err := createWSProxy(holoURL, proxyLogger, "holo")
	if err != nil {
		return nil, err
	}

	return &BotProxies{
		Holo:       holoProxy,
		HoloWS:     holoWSProxy,
		holoAPIKey: holoAPIKey,
		logger:     proxyLogger,
	}, nil
}

func normalizeProxyTargetURL(targetURL string) (*url.URL, bool, error) {
	targetURL = strings.TrimSpace(targetURL)
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, false, fmt.Errorf("parse proxy target URL: %w", err)
	}

	if target.Scheme == "" || target.Host == "" {
		return nil, false, fmt.Errorf("invalid proxy target URL: must be scheme://host, got %q", targetURL)
	}

	normalized := false
	if target.Path != "" || target.RawPath != "" {
		target.Path = ""
		target.RawPath = ""
		normalized = true
	}
	if target.RawQuery != "" {
		target.RawQuery = ""
		target.ForceQuery = false
		normalized = true
	}
	if target.Fragment != "" {
		target.Fragment = ""
		normalized = true
	}

	return target, normalized, nil
}

// newH2CTransport: H2C(HTTP/2 Cleartext) Transport를 생성합니다.
// 내부망에서 TLS 없이 HTTP/2 멀티플렉싱과 헤더 압축 이점을 활용합니다.
func newH2CTransport() http.RoundTripper {
	return &http2.Transport{
		AllowHTTP: true, // plain HTTP에서 HTTP/2 허용
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			// TLS 없이 plain TCP 연결 (H2C)
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

func createProxy(targetURL string, logger *slog.Logger, botName string) (*httputil.ReverseProxy, error) {
	target, normalized, err := normalizeProxyTargetURL(targetURL)
	if err != nil {
		return nil, err
	}
	if normalized {
		// URL 원문은 query/userinfo에 민감 정보 포함 가능성이 있어 구조적으로만 기록한다.
		originalTarget, parseErr := url.Parse(strings.TrimSpace(targetURL))
		logger.Warn("proxy_target_url_normalized",
			slog.String("bot", botName),
			slog.String("scheme", target.Scheme),
			slog.String("host", target.Host),
			slog.String("original_path", func() string {
				if parseErr != nil {
					return ""
				}
				return originalTarget.Path
			}()),
			slog.Bool("had_query", func() bool {
				if parseErr != nil {
					return false
				}
				return originalTarget.RawQuery != ""
			}()),
			slog.Bool("had_fragment", func() bool {
				if parseErr != nil {
					return false
				}
				return originalTarget.Fragment != ""
			}()),
			slog.String("normalized", target.String()),
		)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// H2C (HTTP/2 Cleartext): 내부망에서 멀티플렉싱 및 헤더 압축 활용
	proxy.Transport = otelhttp.NewTransport(newH2CTransport())

	// 에러 핸들러
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"Service unavailable"}`))
	}

	// upstream 404는 라우트(prefix) 불일치 가능성이 높아서 서버 로그로 남긴다.
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusNotFound {
			logger.Warn("proxy_upstream_not_found",
				slog.String("bot", botName),
				slog.String("method", resp.Request.Method),
				slog.String("path", resp.Request.URL.Path),
			)
		}
		return nil
	}

	return proxy, nil
}

// createWSProxy: WebSocket 요청을 위한 HTTP/1.1 프록시를 생성합니다.
// HTTP/2 (H2C)는 Connection: Upgrade 헤더를 지원하지 않으므로 WebSocket에는 HTTP/1.1이 필요합니다.
func createWSProxy(targetURL string, logger *slog.Logger, botName string) (*httputil.ReverseProxy, error) {
	target, _, err := normalizeProxyTargetURL(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// HTTP/1.1 Transport: WebSocket 업그레이드 지원
	proxy.Transport = otelhttp.NewTransport(http.DefaultTransport)

	// WebSocket 연결 에러 핸들러
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("websocket_proxy_error",
			slog.String("bot", botName),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"WebSocket service unavailable"}`))
	}

	return proxy, nil
}

// isWebSocketRequest: 요청이 WebSocket 업그레이드 요청인지 확인합니다.
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// ProxyHolo: hololive-bot으로 프록시
// /admin/api/holo/* → hololive-bot /api/holo/*
func (p *BotProxies) ProxyHolo(c *gin.Context) {
	// 경로 변환: /admin/api/holo/* → /api/holo/*
	originalPath := c.Request.URL.Path
	newPath := strings.Replace(originalPath, "/admin/api/holo", "/api/holo", 1)
	c.Request.URL.Path = newPath

	// X-API-Key 헤더 주입 (hololive-bot 인증용)
	if p.holoAPIKey != "" {
		c.Request.Header.Set("X-API-Key", p.holoAPIKey)
	}

	// WebSocket 요청은 HTTP/1.1 프록시 사용
	if isWebSocketRequest(c.Request) {
		p.logger.Debug("proxy websocket to holo",
			slog.String("original", originalPath),
			slog.String("target", newPath),
		)
		p.HoloWS.ServeHTTP(c.Writer, c.Request)
		return
	}

	p.logger.Debug("proxy to holo",
		slog.String("original", originalPath),
		slog.String("target", newPath),
	)
	p.Holo.ServeHTTP(c.Writer, c.Request)
}
