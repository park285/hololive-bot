package browserfetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	"github.com/park285/shared-go/pkg/jsonutil"
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
		client:   httputil.NewClient(timeout),
		endpoint: strings.TrimSpace(endpoint),
		timeout:  timeout,
	}
}

func (f *Fetcher) FetchPage(ctx context.Context, req Request) (response Response, err error) {
	if f == nil || f.endpoint == "" {
		return Response{}, fmt.Errorf("browser snapshot endpoint is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	httpReq, err := f.newSnapshotRequest(ctx, req)
	if err != nil {
		return Response{}, err
	}

	resp, err := f.doSnapshotRequest(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close browser snapshot response: %w", closeErr))
		}
	}()

	return readSnapshotResponse(resp)
}

func (f *Fetcher) doSnapshotRequest(req *http.Request) (*http.Response, error) {
	resp, err := f.client.Do(req)
	if err != nil {
		if isNilHTTPResponseError(err) {
			return nil, fmt.Errorf("browser snapshot request returned nil response: %w", err)
		}
		return nil, fmt.Errorf("browser snapshot request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("browser snapshot request returned nil response")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("browser snapshot response body is nil")
	}
	return resp, nil
}

func isNilHTTPResponseError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "nil *Response")
}

func (f *Fetcher) newSnapshotRequest(ctx context.Context, req Request) (*http.Request, error) {
	payload, err := json.Marshal(browserSnapshotRequest{URL: req.URL, Headers: req.Header, Screenshot: true})
	if err != nil {
		return nil, fmt.Errorf("marshal browser snapshot request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create browser snapshot request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func readSnapshotResponse(resp *http.Response) (Response, error) {
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
