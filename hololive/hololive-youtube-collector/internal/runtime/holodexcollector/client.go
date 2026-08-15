package holodexcollector

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/httpbody"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	maxBody int64
}

func NewClient(httpClient *http.Client, baseURL, apiKey string, timeout time.Duration, maxBody int64) (*Client, error) {
	parsed, err := parseHTTPSOrigin(baseURL, "holodex")
	if err != nil {
		return nil, err
	}
	timeout, maxBody = defaultClientLimits(timeout, 25*time.Second, maxBody)
	return &Client{
		http:    applyClientTimeout(httpClient, timeout),
		baseURL: strings.TrimRight(parsed.String(), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		maxBody: maxBody,
	}, nil
}

func parseHTTPSOrigin(baseURL, name string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("parse %s base URL: %w", name, err))
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, collecterr.New(collecterr.Failed, name+" base URL must be an HTTPS origin")
	}
	return parsed, nil
}

func defaultClientLimits(timeout, fallback time.Duration, maxBody int64) (resolvedTimeout time.Duration, resolvedMaxBody int64) {
	if timeout <= 0 {
		timeout = fallback
	}
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	return timeout, maxBody
}

func applyClientTimeout(httpClient *http.Client, timeout time.Duration) *http.Client {
	if httpClient == nil {
		return &http.Client{Timeout: timeout}
	}
	if httpClient.Timeout > 0 {
		return httpClient
	}
	cloned := *httpClient
	cloned.Timeout = timeout
	return &cloned
}

func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	if c == nil || c.http == nil {
		return nil, collecterr.New(collecterr.Failed, "holodex client is not configured")
	}
	if c.apiKey == "" {
		return nil, collecterr.New(collecterr.Failed, "holodex api key is not configured")
	}
	endpoint, err := url.Parse(c.baseURL + "/live")
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("build holodex live URL: %w", err))
	}
	query := endpoint.Query()
	query.Set("org", "Hololive")
	query.Set("status", "live,upcoming")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("build holodex request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-APIKEY", c.apiKey)
	req.Header.Set("User-Agent", "HololiveBot/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Join(
			collecterr.FromContext(fmt.Errorf("request holodex live: %w", err)),
			closeResponse(resp),
		)
	}
	if resp == nil || resp.Body == nil {
		return nil, collecterr.New(collecterr.Failed, "holodex response is nil")
	}
	return readJSONBody(resp, c.maxBody, "holodex")
}

func readJSONBody(resp *http.Response, maxBody int64, name string) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, collecterr.New(collecterr.Failed, name+" response is nil")
	}
	body, readErr := httpbody.ReadAllAndDrain(resp.Body, maxBody)
	closeErr := resp.Body.Close()
	err := errors.Join(readErr, closeErr)
	if err != nil {
		if errors.Is(err, httpbody.ErrTooLarge) {
			return nil, errors.Join(collecterr.New(collecterr.ParserDrift, name+" response exceeds body limit"), err)
		}
		return nil, collecterr.FromContext(fmt.Errorf("read %s: %w", name, err))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, collecterr.FromStatus(name, resp.StatusCode, resp.Header.Get("Retry-After"), time.Now().UTC())
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, collecterr.New(collecterr.ParserDrift, name+" content type is not JSON")
	}
	return body, nil
}

func closeResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	drainErr := httpbody.Drain(resp.Body, httpbody.DefaultDrainLimit)
	closeErr := resp.Body.Close()
	return errors.Join(drainErr, closeErr)
}
