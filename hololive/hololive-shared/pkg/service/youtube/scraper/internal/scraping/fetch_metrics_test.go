package scraping

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchPageOnceRecordsFetcherSuccessMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineNetHTTP),
	)

	before := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "success", "none", "200"))
	body, err := client.fetchPageOnce(context.Background(), server.URL)
	after := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "success", "none", "200"))

	require.NoError(t, err)
	assert.Contains(t, body, "ytInitialData")
	assert.Equal(t, float64(1), after-before)
}

func TestFetchPageOnceRecordsFetcherFailureMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineNetHTTP),
	)

	before := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "error", "rate_limited", "429"))
	_, err := client.fetchPageOnce(context.Background(), server.URL)
	after := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "error", "rate_limited", "429"))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, float64(1), after-before)
}
