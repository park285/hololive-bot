package holodexcollector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/providerhttp"
)

type Client struct {
	http   *providerhttp.ProviderHTTPClient
	base   *url.URL
	apiKey string
	policy providerhttp.ProviderResponsePolicy
}

func NewClient(httpClient *providerhttp.ProviderHTTPClient, baseURL, apiKey string, maxBody int64) (*Client, error) {
	if httpClient == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "holodex HTTP client is not configured")
	}
	parsed, err := providerhttp.ParseHolodexBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		http:   httpClient,
		base:   parsed,
		apiKey: strings.TrimSpace(apiKey),
		policy: providerhttp.DefaultJSONPolicy(maxBody),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.http == nil {
		return nil
	}
	return c.http.Close()
}

func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	if c == nil || c.http == nil || c.base == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "holodex client is not configured")
	}
	if c.apiKey == "" {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "holodex api key is not configured")
	}
	endpoint := c.base.JoinPath("live")
	query := endpoint.Query()
	query.Set("org", "Hololive")
	query.Set("status", "live,upcoming")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, providerhttp.RedactError(
			collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("build holodex request: %w", err)),
			c.apiKey,
		)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-APIKEY", c.apiKey)
	req.Header.Set("User-Agent", "HololiveBot/1.0")
	resp, err := c.http.Do(req) //nolint:bodyclose // 응답 본문은 공통 bounded reader가 모든 경로에서 닫는다.
	if err != nil {
		return nil, providerhttp.MapRequestError("request holodex live", err, c.apiKey)
	}
	body, err := providerhttp.ReadProviderJSONDocument(ctx, resp, c.policy, contract.ProviderHolodex)
	return body, providerhttp.RedactError(err, c.apiKey)
}
