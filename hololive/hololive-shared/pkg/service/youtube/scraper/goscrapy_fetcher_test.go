package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tech-engine/goscrapy/pkg/core"
	"golang.org/x/net/html"
)

func TestGoScrapyPageFetcher_ReturnsStatusHeadersAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))
		w.Header().Set("X-Goscrapy-Test", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)

	resp, err := goscrapyPageFetcher{client: client}.FetchPage(context.Background(), pageFetchRequest{
		URL: server.URL,
		Header: http.Header{
			"User-Agent": []string{"test-agent"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", resp.Header.Get("X-Goscrapy-Test"))
	assert.Contains(t, string(resp.Body), "ytInitialData")
}

func TestGoScrapyFetchPageOnce_DoesNotFallbackOn429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)

	_, err := client.fetchPage(context.Background(), server.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestGoScrapyPageFetcher_FallsBackOnlyBeforeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback body"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
	)
	fetcher := goscrapyPageFetcher{
		client:   client,
		runner:   failingGoscrapyRunner{err: errors.New("framework stopped")},
		fallback: netHTTPPageFetcher{client: client},
	}

	resp, err := fetcher.FetchPage(context.Background(), pageFetchRequest{URL: server.URL, Header: http.Header{}})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "fallback body", string(resp.Body))
}

func TestGoScrapyPageFetcher_FallsBackOnExecutorErrorBeforeResponse(t *testing.T) {
	var attempts atomic.Int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			return nil, fmt.Errorf("wrapped transport error: %w", &url.Error{
				Op:  "Get",
				URL: req.URL.String(),
				Err: errors.New("connection refused"),
			})
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("fallback body")),
			Request:    req,
		}, nil
	})

	client := NewClient(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	started := time.Now()
	resp, err := goscrapyPageFetcher{
		client:   client,
		fallback: netHTTPPageFetcher{client: client},
	}.FetchPage(ctx, pageFetchRequest{
		URL:    "http://example.test/watch?v=secret",
		Header: http.Header{},
	})
	elapsed := time.Since(started)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "fallback body", string(resp.Body))
	assert.Equal(t, int32(2), attempts.Load())
	assert.Less(t, elapsed, 250*time.Millisecond)
}

func TestGoScrapyPageFetcher_HonorsContextCancellation(t *testing.T) {
	client := NewClient(WithRateLimiter(NewRateLimiter(0)), WithFetcherEngine(FetcherEngineGoScrapy))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := goscrapyPageFetcher{client: client}.FetchPage(ctx, pageFetchRequest{
		URL:    "https://example.invalid/",
		Header: http.Header{},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "canceled") || errors.Is(err, context.Canceled))
}

type failingGoscrapyRunner struct {
	err error
}

func (r failingGoscrapyRunner) Run(context.Context, *Client, pageFetchRequest) (pageFetchResponse, bool, error) {
	return pageFetchResponse{}, false, r.err
}

func TestGoScrapyPageFetcher_TimeoutBeforeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := goscrapyPageFetcher{client: client}.FetchPage(ctx, pageFetchRequest{URL: server.URL, Header: http.Header{}})
	require.Error(t, err)
}

type closeTrackingReadCloser struct {
	reader io.Reader
	closed bool
}

func (r *closeTrackingReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *closeTrackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type fakeGoScrapyResponse struct {
	statusCode int
	header     http.Header
	body       io.ReadCloser
}

func (r fakeGoScrapyResponse) Header() http.Header { return r.header }
func (r fakeGoScrapyResponse) Body() io.ReadCloser { return r.body }
func (r fakeGoScrapyResponse) Bytes() []byte       { return nil }
func (r fakeGoScrapyResponse) StatusCode() int     { return r.statusCode }
func (r fakeGoScrapyResponse) Cookies() []*http.Cookie {
	return nil
}
func (r fakeGoScrapyResponse) Request() *http.Request { return nil }
func (r fakeGoScrapyResponse) Meta(string) (any, bool) {
	return nil, false
}
func (r fakeGoScrapyResponse) Detach() core.IResponseReader { return r }
func (r fakeGoScrapyResponse) Css(string) core.ISelector    { return r }
func (r fakeGoScrapyResponse) Xpath(string) core.ISelector  { return r }
func (r fakeGoScrapyResponse) Get() *html.Node              { return nil }
func (r fakeGoScrapyResponse) GetAll() []*html.Node         { return nil }
func (r fakeGoScrapyResponse) Text(...string) []string      { return nil }
func (r fakeGoScrapyResponse) Attr(string) []string         { return nil }

func TestReadGoScrapyResponse_ClosesBodyAfterBoundedNonOKRead(t *testing.T) {
	body := &closeTrackingReadCloser{reader: strings.NewReader(strings.Repeat("x", 8192))}

	resp, err := readGoScrapyResponse(fakeGoScrapyResponse{
		statusCode: http.StatusTooManyRequests,
		header:     http.Header{"Retry-After": []string{"1"}},
		body:       body,
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Empty(t, resp.Body)
	assert.True(t, body.closed)
}

func TestSafeFetchError_RedactsURLQueryFromWrappedURLError(t *testing.T) {
	rawURL := "http://example.test/path?token=secret"
	err := fmt.Errorf("goscrapy fetch page: %w", &url.Error{
		Op:  "Get",
		URL: rawURL,
		Err: errors.New("connection refused"),
	})

	safe := safeFetchError(err, rawURL)

	assert.Contains(t, safe, "http://example.test/path")
	assert.NotContains(t, safe, "token=secret")
	assert.NotContains(t, safe, rawURL)
}
