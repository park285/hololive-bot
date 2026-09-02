package providerhttp

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

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
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "http.DefaultTransport is not *http.Transport")
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
			Transport:     transport,
			CheckRedirect: rejectRedirect,
		},
		transport: transport,
		owned:     true,
	}, nil
}

func WrapProviderHTTPDoer(doer HTTPDoer) (*ProviderHTTPClient, error) {
	if doer == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP doer is nil")
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
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP client is not configured")
	}

	if req == nil {
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "provider HTTP request is nil")
	}

	if c.doer != nil {
		out, err := c.doer.Do(req)
		if err != nil {
			return nil, fmt.Errorf("do: %w", err)
		}

		return out, nil
	}

	if c.client == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider HTTP client is not configured")
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
