package httpclient

import (
	"crypto/tls"
	"testing"
	"time"
)

func TestFastConfig(t *testing.T) {
	cfg := FastConfig()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{"Timeout", cfg.Timeout, FastTimeout},
		{"DialTimeout", cfg.DialTimeout, 2 * time.Second},
		{"DialKeepAlive", cfg.DialKeepAlive, 15 * time.Second},
		{"TLSHandshakeTimeout", cfg.TLSHandshakeTimeout, 2 * time.Second},
		{"ResponseHeaderTimeout", cfg.ResponseHeaderTimeout, 3 * time.Second},
		{"ExpectContinueTimeout", cfg.ExpectContinueTimeout, 1 * time.Second},
		{"IdleConnTimeout", cfg.IdleConnTimeout, 60 * time.Second},
		{"MaxIdleConns", cfg.MaxIdleConns, 50},
		{"MaxIdleConnsPerHost", cfg.MaxIdleConnsPerHost, 10},
		{"MaxConnsPerHost", cfg.MaxConnsPerHost, 50},
		{"ForceAttemptHTTP2", cfg.ForceAttemptHTTP2, true},
		{"DisableCompression", cfg.DisableCompression, false},
		{"DisableKeepAlives", cfg.DisableKeepAlives, false},
		{"InsecureSkipVerify", cfg.InsecureSkipVerify, false},
		{"MinTLSVersion", cfg.MinTLSVersion, uint16(tls.VersionTLS12)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestStandardConfig(t *testing.T) {
	cfg := StandardConfig()
	defaultCfg := DefaultConfig()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{"Timeout", cfg.Timeout, defaultCfg.Timeout},
		{"DialTimeout", cfg.DialTimeout, defaultCfg.DialTimeout},
		{"MaxIdleConns", cfg.MaxIdleConns, defaultCfg.MaxIdleConns},
		{"ForceAttemptHTTP2", cfg.ForceAttemptHTTP2, defaultCfg.ForceAttemptHTTP2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestLongRunningConfig(t *testing.T) {
	cfg := LongRunningConfig()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{"Timeout", cfg.Timeout, LLMTimeout},
		{"DialTimeout", cfg.DialTimeout, 10 * time.Second},
		{"DialKeepAlive", cfg.DialKeepAlive, 60 * time.Second},
		{"TLSHandshakeTimeout", cfg.TLSHandshakeTimeout, 10 * time.Second},
		{"ResponseHeaderTimeout", cfg.ResponseHeaderTimeout, 30 * time.Second},
		{"ExpectContinueTimeout", cfg.ExpectContinueTimeout, 2 * time.Second},
		{"IdleConnTimeout", cfg.IdleConnTimeout, 180 * time.Second},
		{"MaxIdleConns", cfg.MaxIdleConns, 256},
		{"MaxIdleConnsPerHost", cfg.MaxIdleConnsPerHost, 128},
		{"MaxConnsPerHost", cfg.MaxConnsPerHost, 256},
		{"ForceAttemptHTTP2", cfg.ForceAttemptHTTP2, true},
		{"DisableCompression", cfg.DisableCompression, false},
		{"DisableKeepAlives", cfg.DisableKeepAlives, false},
		{"InsecureSkipVerify", cfg.InsecureSkipVerify, false},
		{"MinTLSVersion", cfg.MinTLSVersion, uint16(tls.VersionTLS12)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      time.Duration
		expected time.Duration
	}{
		{"DefaultTimeout", DefaultTimeout, 30 * time.Second},
		{"DefaultDialTimeout", DefaultDialTimeout, 5 * time.Second},
		{"FastTimeout", FastTimeout, 3 * time.Second},
		{"LLMTimeout", LLMTimeout, 120 * time.Second},
		{"DefaultRetryBackoff", DefaultRetryBackoff, 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}

	intTests := []struct {
		name     string
		got      int
		expected int
	}{
		{"DefaultIdleConns", DefaultIdleConns, 100},
		{"DefaultMaxConnsPerHost", DefaultMaxConnsPerHost, 100},
	}

	for _, tt := range intTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d, expected %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestFactoryFunctionsReturnNewInstances(t *testing.T) {
	// 각 팩토리 함수는 독립적인 Config 인스턴스를 반환해야 함
	fast1 := FastConfig()
	fast2 := FastConfig()

	// 값은 같지만 서로 다른 인스턴스여야 함
	if fast1.Timeout != fast2.Timeout {
		t.Error("FastConfig should return consistent values")
	}

	// 수정해도 다른 인스턴스에 영향을 주지 않아야 함
	fast1.Timeout = 1 * time.Second
	if fast2.Timeout == 1*time.Second {
		t.Error("FastConfig instances should be independent")
	}
}

func TestConfigCreationWithNew(t *testing.T) {
	// 생성된 Config로 실제 HTTP 클라이언트를 만들 수 있는지 검증
	configs := []Config{
		FastConfig(),
		StandardConfig(),
		LongRunningConfig(),
	}

	for i, cfg := range configs {
		client := New(cfg)
		if client == nil {
			t.Errorf("Config %d: New() returned nil", i)
		}
		if client.Timeout != cfg.Timeout {
			t.Errorf("Config %d: Client timeout = %v, expected %v", i, client.Timeout, cfg.Timeout)
		}
	}
}
