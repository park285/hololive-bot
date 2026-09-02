package officialcollector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/providerhttp"
)

type Client struct {
	http   *providerhttp.ProviderHTTPClient
	base   *url.URL
	policy providerhttp.ProviderResponsePolicy
}

func NewClient(httpClient *providerhttp.ProviderHTTPClient, baseURL string, maxBody int64) (*Client, error) {
	if httpClient == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "official schedule HTTP client is not configured")
	}

	parsed, err := providerhttp.ParseOfficialScheduleBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse official schedule base URL: %w", err)
	}

	return &Client{
		http:   httpClient,
		base:   parsed,
		policy: providerhttp.DefaultJSONPolicy(maxBody),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.http == nil {
		return nil
	}

	if err := c.http.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	if c == nil || c.http == nil || c.base == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "official schedule client is not configured")
	}

	endpoint := c.base.JoinPath("api", "list", "2")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("build official schedule request: %w", err))
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")

	resp, err := c.http.Do(req) //nolint:bodyclose // 응답 본문은 공통 bounded reader가 모든 경로에서 닫는다.
	if err != nil {
		if mapErr := providerhttp.MapRequestError("request official schedule", err); mapErr != nil {
			return nil, fmt.Errorf("map request error: %w", mapErr)
		}

		return nil, nil
	}

	out, err := providerhttp.ReadProviderJSONDocument(ctx, resp, c.policy, contract.ProviderHololiveOfficial)
	if err != nil {
		return out, fmt.Errorf("read provider JSON document: %w", err)
	}

	return out, nil
}
