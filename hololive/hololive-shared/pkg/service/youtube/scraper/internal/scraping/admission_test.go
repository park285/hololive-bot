package scraping

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFetchPagePreflight_RateLimitDenialReturnsAdmissionDeferred(t *testing.T) {
	limiter := NewRateLimiter(time.Hour)
	client := NewClient(WithRateLimiter(limiter))
	pageURL := "https://www.youtube.com/channel/UC123/community"

	require.NoError(t, client.fetchPagePreflight(context.Background(), pageURL))

	err := client.fetchPagePreflight(context.Background(), pageURL)
	require.Error(t, err)
	require.True(t, IsAdmissionDeferred(err), "err = %v", err)

	var deferred *AdmissionDeferredError
	require.ErrorAs(t, err, &deferred)
	require.Greater(t, deferred.RetryDelay(), time.Duration(0))
	require.NotEmpty(t, deferred.Bucket)
}

func TestClassifyFailure_AdmissionDeferred(t *testing.T) {
	delay := 3 * time.Second
	err := fmt.Errorf("wrapped: %w", &AdmissionDeferredError{
		Source:     "test",
		Reason:     "local_interval",
		RetryAfter: delay,
	})

	detail := ClassifyFailure(err, FailureSourceHTML)
	require.Equal(t, FailureReasonAdmissionDeferred, detail.Reason)
	require.Equal(t, delay, detail.RetryAfter)
}

func TestIsRetryableFetchPageError_AdmissionDeferredIsNotRetryable(t *testing.T) {
	err := &AdmissionDeferredError{Source: "test", RetryAfter: time.Second}
	require.False(t, isRetryableFetchPageError(err))
}

func TestFetchPageAdmissionDeferred_IsNotFetchAttemptTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	limiter := NewRateLimiter(time.Hour)
	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(limiter),
	)

	_, err := client.fetchPage(context.Background(), server.URL, FetchPolicy{MaxAttempts: 1, PerAttemptTimeout: time.Second})
	require.NoError(t, err)

	_, err = client.fetchPage(context.Background(), server.URL, FetchPolicy{MaxAttempts: 1, PerAttemptTimeout: time.Nanosecond})
	require.Error(t, err)
	require.True(t, IsAdmissionDeferred(err), "err = %v", err)
	require.False(t, errors.Is(err, errFetchAttemptTimeout), "err = %v", err)
}
