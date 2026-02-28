package httpclient

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http2"
)

type Config struct {
	Timeout time.Duration

	DialTimeout   time.Duration
	DialKeepAlive time.Duration

	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration

	IdleConnTimeout time.Duration

	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int

	ForceAttemptHTTP2  bool
	DisableCompression bool
	DisableKeepAlives  bool
	InsecureSkipVerify bool
	MinTLSVersion      uint16

	HTTP2ReadIdleTimeout time.Duration // 유휴 연결 PING 시작 간격 (0=비활성)
	HTTP2PingTimeout     time.Duration // PING 응답 대기 타임아웃 (0=기본값)
}

func DefaultConfig() Config {
	return Config{
		Timeout: 30 * time.Second,

		DialTimeout:   5 * time.Second,
		DialKeepAlive: 30 * time.Second,

		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		IdleConnTimeout: 90 * time.Second,

		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		MaxConnsPerHost:     0,

		ForceAttemptHTTP2:  true,
		DisableCompression: false,
		DisableKeepAlives:  false,

		InsecureSkipVerify: false,
		MinTLSVersion:      tls.VersionTLS12,
	}
}

func New(cfg Config) *http.Client {
	tr := NewTransport(cfg)
	return &http.Client{
		Transport: tr,
		Timeout:   cfg.Timeout,
	}
}

func NewTransport(cfg Config) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   cfg.DialTimeout,
		KeepAlive: cfg.DialKeepAlive,
	}

	insecureSkipVerify := resolveInsecureSkipVerify(cfg)

	minTLSVersion := cfg.MinTLSVersion
	if minTLSVersion < tls.VersionTLS12 {
		if minTLSVersion != 0 {
			slog.Warn(
				"MinTLSVersion below TLS1.2 requested; enforcing TLS1.2",
				"requested", minTLSVersion,
				"enforced", tls.VersionTLS12,
			)
		}
		minTLSVersion = tls.VersionTLS12
	}

	tlsCfg := &tls.Config{
		MinVersion:         minTLSVersion,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // 환경 변수 기반 명시적 opt-in
	}

	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,

		DialContext: dialer.DialContext,

		ForceAttemptHTTP2: cfg.ForceAttemptHTTP2,

		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,

		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		ExpectContinueTimeout: cfg.ExpectContinueTimeout,

		DisableCompression: cfg.DisableCompression,
		DisableKeepAlives:  cfg.DisableKeepAlives,

		TLSClientConfig: tlsCfg,
	}
	configureHTTP2Ping(tr, cfg)
	return tr
}

// configureHTTP2Ping: HTTP/2 PING 헬스체크 설정 (zombie 연결 조기 탐지)
func configureHTTP2Ping(tr *http.Transport, cfg Config) {
	if cfg.HTTP2ReadIdleTimeout <= 0 {
		return
	}
	h2t, err := http2.ConfigureTransports(tr)
	if err != nil {
		slog.Warn("http2 PING 설정 실패, 기본 동작 유지", "error", err)
		return
	}
	h2t.ReadIdleTimeout = cfg.HTTP2ReadIdleTimeout
	if cfg.HTTP2PingTimeout > 0 {
		h2t.PingTimeout = cfg.HTTP2PingTimeout
	}
}

func resolveInsecureSkipVerify(cfg Config) bool {
	// SECURITY: InsecureSkipVerify는 테스트/개발 전용입니다.
	// 프로덕션에서는 항상 fail-closed(검증 유지) 처리합니다.
	if !cfg.InsecureSkipVerify {
		return false
	}

	allowInsecure := strings.ToLower(strings.TrimSpace(os.Getenv("HTTP_ALLOW_INSECURE_TLS")))
	if allowInsecure != "true" {
		slog.Info(
			"InsecureSkipVerify requested but denied - HTTP_ALLOW_INSECURE_TLS not set",
			"config_value", cfg.InsecureSkipVerify,
			"result", "TLS verification enabled (secure)",
		)
		return false
	}

	env := strings.TrimSpace(os.Getenv("OTEL_ENVIRONMENT"))
	isProduction := env == "" || strings.EqualFold(env, "production")
	if isProduction {
		slog.Error(
			"InsecureSkipVerify blocked in production environment",
			"is_production", true,
			"config_value", cfg.InsecureSkipVerify,
			"env_var", "HTTP_ALLOW_INSECURE_TLS=true",
			"security_risk", "MITM attacks - TLS verification kept enabled",
		)
		return false
	}

	slog.Warn(
		"TLS verification disabled - NOT for production use",
		"config_value", cfg.InsecureSkipVerify,
		"env_var", "HTTP_ALLOW_INSECURE_TLS=true",
		"security_risk", "vulnerable to MITM attacks",
	)
	return true
}

func CloneClient(base *http.Client) *http.Client {
	if base == nil {
		return nil
	}
	cp := *base
	if tr, ok := base.Transport.(*http.Transport); ok && tr != nil {
		cp.Transport = tr.Clone()
	}
	return new(cp)
}

func CloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}
