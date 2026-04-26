package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingPageFetcher struct {
	err error
}

func (f failingPageFetcher) FetchPage(context.Context, pageFetchRequest) (pageFetchResponse, error) {
	return pageFetchResponse{}, f.err
}

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
