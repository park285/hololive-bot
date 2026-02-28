package httpclient

import (
	"crypto/tls"
	"net/http"
	"time"
)

// 표준 타임아웃 상수
const (
	// DefaultTimeout은 일반적인 HTTP 요청의 기본 타임아웃입니다.
	DefaultTimeout = 30 * time.Second

	// DefaultDialTimeout은 TCP 연결 수립 시 기본 타임아웃입니다.
	DefaultDialTimeout = 5 * time.Second

	// FastTimeout은 빠른 응답이 예상되는 경량 API 호출용 타임아웃입니다.
	FastTimeout = 3 * time.Second

	// LLMTimeout은 LLM 추론 등 장시간 실행되는 작업용 타임아웃입니다.
	LLMTimeout = 120 * time.Second

	// DefaultIdleConns는 모든 호스트에 대한 유휴 연결 총 개수입니다.
	DefaultIdleConns = 100

	// DefaultMaxConnsPerHost는 호스트당 최대 연결 수입니다.
	DefaultMaxConnsPerHost = 100

	// DefaultRetryBackoff는 재시도 시 기본 백오프 간격입니다.
	DefaultRetryBackoff = 500 * time.Millisecond
)

// FastConfig는 빠른 응답이 예상되는 API 호출용 설정을 반환합니다.
// - 3초 타임아웃
// - 빠른 다이얼 및 TLS handshake
// - HTTP/2 활성화
func FastConfig() Config {
	return Config{
		Timeout: FastTimeout,

		DialTimeout:   2 * time.Second,
		DialKeepAlive: 15 * time.Second,

		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		IdleConnTimeout: 60 * time.Second,

		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     50,

		ForceAttemptHTTP2:  true,
		DisableCompression: false,
		DisableKeepAlives:  false,

		InsecureSkipVerify: false,
		MinTLSVersion:      tls.VersionTLS12,
	}
}

// StandardConfig는 일반적인 HTTP 요청용 표준 설정을 반환합니다.
// - 30초 타임아웃
// - 밸런스된 연결 풀 크기
// - DefaultConfig와 동일한 설정
func StandardConfig() Config {
	return DefaultConfig()
}

// LongRunningConfig는 LLM 추론 등 장시간 실행되는 작업용 설정을 반환합니다.
// - 120초 타임아웃
// - 더 긴 응답 대기 시간
// - 안정적인 연결 유지
func LongRunningConfig() Config {
	return Config{
		Timeout: LLMTimeout,

		DialTimeout:   10 * time.Second,
		DialKeepAlive: 60 * time.Second,

		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,

		IdleConnTimeout: 180 * time.Second,

		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 128,
		MaxConnsPerHost:     256,

		ForceAttemptHTTP2:  true,
		DisableCompression: false,
		DisableKeepAlives:  false,

		InsecureSkipVerify: false,
		MinTLSVersion:      tls.VersionTLS12,
	}
}

func NewWithTimeout(timeout time.Duration) *http.Client {
	cfg := DefaultConfig()
	cfg.Timeout = timeout
	return New(cfg)
}
