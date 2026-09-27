package scraping

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/admission"
	ratelimiter "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestFetchPagePreflight_RateLimitDenialReturnsAdmissionDeferred(t *testing.T) {
	limiter := ratelimiter.New(time.Hour)
	client := NewClient(testYouTubeConfig(), WithRateLimiter(limiter))
	pageURL := "https://www.youtube.com/channel/UC123/community"

	require.NoError(t, client.fetchPagePreflight(t.Context(), pageURL))

	err := client.fetchPagePreflight(t.Context(), pageURL)
	require.Error(t, err)
	require.True(t, youtubeadmission.IsDeferred(err), "err = %v", err)

	var deferred *youtubeadmission.DeferredError

	require.ErrorAs(t, err, &deferred)
	require.NotNil(t, deferred)
	require.Greater(t, deferred.RetryDelay(), time.Duration(0))
	require.NotEmpty(t, deferred.Bucket)
}

func TestClassifyFailure_AdmissionDeferred(t *testing.T) {
	delay := 3 * time.Second
	err := fmt.Errorf("wrapped: %w", &youtubeadmission.DeferredError{
		Source:     "test",
		Reason:     "local_interval",
		RetryAfter: delay,
	})

	detail := ClassifyFailure(err, FailureSourceHTML)
	require.Equal(t, FailureReasonAdmissionDeferred, detail.Reason)
	require.Equal(t, delay, detail.RetryAfter)
}

func TestIsRetryableFetchPageError_AdmissionDeferredIsNotRetryable(t *testing.T) {
	err := &youtubeadmission.DeferredError{Source: "test", RetryAfter: time.Second}
	require.False(t, isRetryableFetchPageError(err))
}

func TestFetchPageAdmissionDeferred_IsNotFetchAttemptTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
	}))
	defer server.Close()

	limiter := ratelimiter.New(time.Hour)
	client := NewClient(testYouTubeConfig(),
		WithHTTPClient(server.Client()),
		WithRateLimiter(limiter),
	)

	_, err := client.fetchPage(t.Context(), server.URL, FetchPolicy{MaxAttempts: 1, PerAttemptTimeout: time.Second})
	require.NoError(t, err)

	_, err = client.fetchPage(t.Context(), server.URL, FetchPolicy{MaxAttempts: 1, PerAttemptTimeout: time.Nanosecond})
	require.Error(t, err)
	require.True(t, youtubeadmission.IsDeferred(err), "err = %v", err)
	require.NotErrorIs(t, err, errFetchAttemptTimeout, "err = %v", err)
}
