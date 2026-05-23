package browserfetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/jsonutil"
)

type Config struct {
	Enabled  bool
	Endpoint string
	Timeout  time.Duration
}

type Fetcher struct {
	client   *http.Client
	endpoint string
	timeout  time.Duration
}

type Request struct {
	URL    string
	Header http.Header
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
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

func New(endpoint string, timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Fetcher{
		client:   &http.Client{Timeout: timeout},
		endpoint: strings.TrimSpace(endpoint),
		timeout:  timeout,
	}
}

func (f *Fetcher) FetchPage(ctx context.Context, req Request) (Response, error) {
	if f == nil || f.endpoint == "" {
		return Response{}, fmt.Errorf("browser snapshot endpoint is not configured")
	}
	payload, err := json.Marshal(browserSnapshotRequest{URL: req.URL, Headers: req.Header, Screenshot: true})
	if err != nil {
		return Response{}, fmt.Errorf("marshal browser snapshot request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create browser snapshot request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("browser snapshot request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := jsonutil.ReadAllLimit(resp.Body, 4<<20)
	if err != nil {
		return Response{}, fmt.Errorf("read browser snapshot response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, fmt.Errorf("browser snapshot unexpected status: %d", resp.StatusCode)
	}
	var parsed browserSnapshotResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, fmt.Errorf("decode browser snapshot response: %w", err)
	}
	return Response{StatusCode: parsed.StatusCode, Header: parsed.Header, Body: []byte(parsed.HTML)}, nil
}
