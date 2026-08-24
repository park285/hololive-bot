package scraping

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ratelimiter "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

const floatCompareDelta = 1e-9

func TestFetchPageOnceRecordsFetcherSuccessMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(ratelimiter.New(0)),
		WithFetcherEngine(FetcherEngineNetHTTP),
	)

	before := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "success", "none", "200"))
	body, err := client.fetchPageOnce(t.Context(), server.URL)
	after := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "success", "none", "200"))

	require.NoError(t, err)
	assert.Contains(t, body, "ytInitialData")
	assert.InDelta(t, float64(1), after-before, floatCompareDelta)
}

func TestFetchPageOnceRecordsFetcherFailureMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(ratelimiter.New(0)),
		WithFetcherEngine(FetcherEngineNetHTTP),
	)

	before := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "error", "rate_limited", "429"))
	_, err := client.fetchPageOnce(t.Context(), server.URL)
	after := testutil.ToFloat64(scraperFetchRequestsTotal.WithLabelValues("nethttp", "error", "rate_limited", "429"))

	require.Error(t, err)
	require.ErrorIs(t, err, ErrRateLimited)
	assert.InDelta(t, float64(1), after-before, floatCompareDelta)
}
