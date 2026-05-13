package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
)

type BrowserSnapshotConfig struct {
	Enabled  bool
	Endpoint string
	Timeout  time.Duration
}

type BrowserSnapshotFetcher struct {
	client   *http.Client
	endpoint string
	timeout  time.Duration
}

type browserSnapshotRequest struct {
	URL        string      `json:"url"`
	Headers    http.Header `json:"headers,omitempty"`
	Screenshot bool        `json:"screenshot"`
}

type browserSnapshotResponse struct {
	StatusCode int         `json:"status_code"`
	HTML       string      `json:"html"`
	Screenshot []byte      `json:"screenshot,omitempty"`
	Header     http.Header `json:"header,omitempty"`
}

func NewBrowserSnapshotFetcher(endpoint string, timeout time.Duration) *BrowserSnapshotFetcher {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &BrowserSnapshotFetcher{
		client:   &http.Client{Timeout: timeout},
		endpoint: strings.TrimSpace(endpoint),
		timeout:  timeout,
	}
}

func (f *BrowserSnapshotFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	if f == nil || f.endpoint == "" {
		return pageFetchResponse{}, fmt.Errorf("browser snapshot endpoint is not configured")
	}
	payload, err := json.Marshal(browserSnapshotRequest{URL: req.URL, Headers: req.Header, Screenshot: true})
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("marshal browser snapshot request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(payload))
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("create browser snapshot request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(httpReq)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("browser snapshot request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := jsonutil.ReadAllLimit(resp.Body, 4<<20)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("read browser snapshot response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return pageFetchResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, fmt.Errorf("browser snapshot unexpected status: %d", resp.StatusCode)
	}
	var parsed browserSnapshotResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return pageFetchResponse{}, fmt.Errorf("decode browser snapshot response: %w", err)
	}
	return pageFetchResponse{StatusCode: parsed.StatusCode, Header: parsed.Header, Body: []byte(parsed.HTML)}, nil
}
