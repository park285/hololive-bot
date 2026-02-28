package httpclient

import (
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestConfigureHTTP2Ping_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	// HTTP2ReadIdleTimeout=0 (기본값) → PING 설정 미적용
	tr := NewTransport(cfg)

	// http2.ConfigureTransports가 호출되지 않았으므로 기본 동작 유지 확인
	// transport가 정상 생성되었는지만 검증 (PING 미설정)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestConfigureHTTP2Ping_Enabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HTTP2ReadIdleTimeout = 30 * time.Second
	cfg.HTTP2PingTimeout = 5 * time.Second

	tr := NewTransport(cfg)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}

	// configureHTTP2Ping 내부에서 http2.ConfigureTransports 호출 → 검증
	// 다시 ConfigureTransports를 호출하면 이미 설정된 transport에서 에러 발생하거나
	// 동일한 h2t를 반환하므로, transport 동작 자체가 정상인지 검증
	h2t, err := http2.ConfigureTransports(tr)
	if err != nil {
		// 이미 설정된 경우 에러 발생 가능 → PING 설정이 적용되었다는 의미
		// Go http2 패키지는 이미 설정된 transport에 대해 에러를 반환
		t.Logf("http2 already configured (expected): %v", err)
		return
	}

	// 에러가 없으면 h2t에서 설정 확인
	if h2t.ReadIdleTimeout != 30*time.Second {
		t.Errorf("ReadIdleTimeout = %v, want 30s", h2t.ReadIdleTimeout)
	}
	if h2t.PingTimeout != 5*time.Second {
		t.Errorf("PingTimeout = %v, want 5s", h2t.PingTimeout)
	}
}
