package officialcollector

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

const officialScheduleAPIPath = "/api/list/2"

type Client struct {
	http    *http.Client
	baseURL string
	maxBody int64
}

func NewClient(httpClient *http.Client, baseURL string, timeout time.Duration, maxBody int64) (*Client, error) {
	parsed, err := parseOfficialBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	timeout, maxBody = defaultOfficialLimits(timeout, maxBody)
	return &Client{http: applyOfficialTimeout(httpClient, timeout), baseURL: strings.TrimRight(parsed.String(), "/"), maxBody: maxBody}, nil
}

func parseOfficialBaseURL(baseURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("parse official schedule base URL: %w", err))
	}
	if !validOfficialOrigin(parsed) {
		return nil, collecterr.New(collecterr.Failed, "official schedule base URL must be an HTTPS origin")
	}
	return parsed, nil
}

func validOfficialOrigin(parsed *url.URL) bool {
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return false
	}
	return parsed.RawQuery == "" && parsed.Fragment == ""
}

func defaultOfficialLimits(timeout time.Duration, maxBody int64) (time.Duration, int64) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	return timeout, maxBody
}

func applyOfficialTimeout(httpClient *http.Client, timeout time.Duration) *http.Client {
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
		return nil, collecterr.New(collecterr.Failed, "official schedule client is not configured")
	}
	endpoint := c.baseURL + officialScheduleAPIPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("build official schedule request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("request official schedule: %w", err))
	}
	defer resp.Body.Close()
	return readOfficialJSON(resp, c.maxBody)
}

func readOfficialJSON(resp *http.Response, maxBody int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read official schedule: %w", err))
	}
	if int64(len(body)) > maxBody {
		return nil, collecterr.New(collecterr.ParserDrift, "official schedule response exceeds body limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, collecterr.FromStatus("official schedule", resp.StatusCode, resp.Header.Get("Retry-After"), time.Now().UTC())
	}
	return requireOfficialJSON(resp, body)
}

func requireOfficialJSON(resp *http.Response, body []byte) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, collecterr.New(collecterr.ParserDrift, "official schedule content type is not JSON")
	}
	return body, nil
}
