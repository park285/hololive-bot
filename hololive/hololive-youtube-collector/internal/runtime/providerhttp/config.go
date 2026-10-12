package providerhttp

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP timeouts must be positive")
	}

	if cfg.MaxConnsPerHost < 1 {
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP MaxConnsPerHost must be at least 1")
	}

	if cfg.MaxIdleConnsPerHost < 1 || cfg.MaxIdleConnsPerHost > cfg.MaxConnsPerHost {
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP idle connection cap is invalid")
	}

	return nil
}
