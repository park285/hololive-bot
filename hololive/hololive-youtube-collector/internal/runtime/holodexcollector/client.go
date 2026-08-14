package holodexcollector

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	maxBody int64
}

func NewClient(httpClient *http.Client, baseURL, apiKey string, timeout time.Duration, maxBody int64) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("parse holodex base URL: %w", err))
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, collecterr.New(collecterr.Failed, "holodex base URL must be an HTTPS origin")
	}
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout <= 0 {
		cloned := *httpClient
		cloned.Timeout = timeout
		httpClient = &cloned
	}
	return &Client{
		http:    httpClient,
		baseURL: strings.TrimRight(parsed.String(), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		maxBody: maxBody,
	}, nil
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
		return nil, collecterr.FromContext(fmt.Errorf("request holodex live: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read holodex live: %w", err))
	}
	if int64(len(body)) > c.maxBody {
		return nil, collecterr.New(collecterr.ParserDrift, "holodex response exceeds body limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, collecterr.FromStatus("holodex", resp.StatusCode, resp.Header.Get("Retry-After"), time.Now().UTC())
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, collecterr.New(collecterr.ParserDrift, "holodex content type is not JSON")
	}
	return body, nil
}
