package providerhttp

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/httpclientmetrics"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ProviderHTTPClient struct {
	client    *http.Client
	transport *http.Transport
	doer      HTTPDoer
	owned     bool
	closeOnce sync.Once
}

func NewProviderHTTPClient(cfg ProviderTransportConfig) (*ProviderHTTPClient, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "http.DefaultTransport is not *http.Transport")
	}

	transport := base.Clone()

	transport.MaxConnsPerHost = cfg.MaxConnsPerHost
	transport.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
	transport.IdleConnTimeout = cfg.IdleConnTimeout
	transport.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	transport.TLSHandshakeTimeout = cfg.TLSHandshakeTimeout
	transport.ExpectContinueTimeout = time.Second
	transport.DisableCompression = false
	transport.DialContext = (&net.Dialer{Timeout: cfg.DialTimeout, KeepAlive: 30 * time.Second}).DialContext

	return &ProviderHTTPClient{
		client: &http.Client{
			Timeout:       cfg.RequestTimeout,
			Transport:     httpclientmetrics.Instrument(cfg.MetricsClient, transport),
			CheckRedirect: rejectRedirect,
		},
		// Close는 계측 wrapper가 아니라 실제 연결을 가진 *http.Transport의 유휴 연결을 닫는다.
		transport: transport,
		owned:     true,
	}, nil
}

func WrapProviderHTTPDoer(doer HTTPDoer) (*ProviderHTTPClient, error) {
	if doer == nil {
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP doer is nil")
	}

	if client, ok := doer.(*http.Client); ok {
		cloned := *client

		cloned.CheckRedirect = rejectRedirect

		return &ProviderHTTPClient{client: &cloned, owned: false}, nil
	}

	return &ProviderHTTPClient{doer: doer, owned: false}, nil
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func (c *ProviderHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c == nil {
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP client is not configured")
	}

	if req == nil {
		return nil, collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, "provider HTTP request is nil")
	}

	if c.doer != nil {
		out, err := c.doer.Do(req)
		if err != nil {
			return nil, fmt.Errorf("do: %w", err)
		}

		return out, nil
	}

	if c.client == nil {
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider HTTP client is not configured")
	}

	out, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}

	return out, nil
}

func (c *ProviderHTTPClient) Close() error {
	if c == nil || !c.owned {
		return nil
	}

	c.closeOnce.Do(func() {
		if c.transport != nil {
			c.transport.CloseIdleConnections()
		}
	})

	return nil
}
