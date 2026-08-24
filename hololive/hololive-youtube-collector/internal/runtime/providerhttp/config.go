package providerhttp

import (
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type ProviderTransportConfig struct {
	RequestTimeout        time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxConnsPerHost       int
	MaxIdleConnsPerHost   int
}

func (cfg ProviderTransportConfig) validate() error {
	if cfg.RequestTimeout <= 0 || cfg.DialTimeout <= 0 || cfg.TLSHandshakeTimeout <= 0 ||
		cfg.ResponseHeaderTimeout <= 0 || cfg.IdleConnTimeout <= 0 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP timeouts must be positive")
	}

	if cfg.MaxConnsPerHost < 1 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP MaxConnsPerHost must be at least 1")
	}

	if cfg.MaxIdleConnsPerHost < 1 || cfg.MaxIdleConnsPerHost > cfg.MaxConnsPerHost {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP idle connection cap is invalid")
	}

	return nil
}
