// Package httputil: HTTP 클라이언트 공통 유틸리티
package httputil

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

type TransportProfile struct {
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxConnsPerHost       int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
}

// NewClient: 지정 타임아웃의 HTTP 클라이언트 반환
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// NewProfiledClient: http.DefaultTransport를 clone한 뒤 필요한 transport 필드만 선택적으로 override합니다.
// 기본 keep-alive, proxy, TLS 기본 동작은 유지하고 timeout/pool/HTTP2 정책만 profile로 주입합니다.
func NewProfiledClient(profile TransportProfile) *http.Client {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || baseTransport == nil {
		baseTransport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	transport := baseTransport.Clone()

	if profile.DialTimeout > 0 {
		transport.DialContext = (&net.Dialer{
			Timeout: profile.DialTimeout,
		}).DialContext
	}
	if profile.TLSHandshakeTimeout > 0 {
		transport.TLSHandshakeTimeout = profile.TLSHandshakeTimeout
	}
	if profile.ResponseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = profile.ResponseHeaderTimeout
	}
	if profile.IdleConnTimeout > 0 {
		transport.IdleConnTimeout = profile.IdleConnTimeout
	}
	if profile.MaxConnsPerHost > 0 {
		transport.MaxConnsPerHost = profile.MaxConnsPerHost
	}
	if profile.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = profile.MaxIdleConnsPerHost
	}
	if profile.DisableHTTP2 {
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}

	return &http.Client{
		Timeout:   profile.Timeout,
		Transport: transport,
	}
}

// NewExternalAPIClient: 일반 외부 API 호출용 프로파일을 적용한 클라이언트를 반환합니다.
func NewExternalAPIClient(timeout time.Duration) *http.Client {
	return NewProfiledClient(TransportProfile{
		Timeout:               timeout,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxConnsPerHost:       32,
		MaxIdleConnsPerHost:   16,
	})
}

// NewInternalServiceClient: 서비스 간 HTTP 호출용 공통 프로파일을 적용한 클라이언트를 반환합니다.
func NewInternalServiceClient(timeout time.Duration) *http.Client {
	return NewProfiledClient(TransportProfile{
		Timeout:               timeout,
		DialTimeout:           3 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxConnsPerHost:       64,
		MaxIdleConnsPerHost:   32,
	})
}

// DefaultClient: 30초 타임아웃 기본 클라이언트 반환
func DefaultClient() *http.Client {
	return NewExternalAPIClient(30 * time.Second)
}
