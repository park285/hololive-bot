package scraping

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/jsonutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

func TestFetchPageRetriesEmptySuccessfulResponse(t *testing.T) {
	var attempts atomic.Int32
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				statusBody := ""
				if attempts.Add(1) == 2 {
					statusBody = "<html>ok</html>"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(statusBody)),
					Request:    req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	body, err := client.fetchPage(context.Background(), "https://www.youtube.com/test", FetchPolicy{
		MaxAttempts:       2,
		PerAttemptTimeout: time.Second,
		BaseDelay:         time.Millisecond,
		MaxDelay:          time.Millisecond,
	})
	require.NoError(t, err)
	require.Equal(t, "<html>ok</html>", body)
	require.Equal(t, int32(2), attempts.Load())
}

func TestFetchPageDoesNotRetryBlockedSuccessfulResponse(t *testing.T) {
	var attempts atomic.Int32
	sorryURL, err := url.Parse("https://www.google.com/sorry/index?continue=...")
	require.NoError(t, err)
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts.Add(1)
				redirected := req.Clone(req.Context())
				redirected.URL = sorryURL
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html>captcha challenge</html>")),
					Request:    redirected,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	_, err = client.fetchPage(context.Background(), "https://www.youtube.com/test", FetchPolicy{
		MaxAttempts:       3,
		PerAttemptTimeout: time.Second,
		BaseDelay:         time.Millisecond,
		MaxDelay:          time.Millisecond,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBlockedResponse)
	require.Equal(t, int32(1), attempts.Load())
}

func TestFinalURLLooksBlockedDetectsRedirectToSorry(t *testing.T) {
	blocked := []string{
		"https://www.google.com/sorry/index?continue=https://youtube.com",
		"https://consent.youtube.com/m?continue=...",
		"https://consent.google.com/ml?continue=...",
		"https://www.youtube.com/sorry/index",
	}
	for _, finalURL := range blocked {
		require.True(t, finalURLLooksBlocked(finalURL), "redirect-to-block final URL must classify blocked: %s", finalURL)
	}

	allowed := []string{
		"",
		"https://www.youtube.com/watch?v=abc",
		"https://www.youtube.com/@member/community",
		"https://www.youtube.com/post/Ug-xyz",
	}
	for _, finalURL := range allowed {
		require.False(t, finalURLLooksBlocked(finalURL), "normal final URL must not classify blocked: %s", finalURL)
	}
}

func TestFetchPagePerAttemptTimeoutRecoversOnRetry(t *testing.T) {
	var attempts atomic.Int32
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if attempts.Add(1) == 1 {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html>ok</html>")),
					Request:    req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	body, err := client.fetchPage(context.Background(), "https://www.youtube.com/test", FetchPolicy{
		MaxAttempts:       2,
		PerAttemptTimeout: 5 * time.Millisecond,
		BaseDelay:         time.Millisecond,
		MaxDelay:          time.Millisecond,
	})
	require.NoError(t, err)
	require.Equal(t, "<html>ok</html>", body)
	require.Equal(t, int32(2), attempts.Load())
}

func TestFetchPageBodyReadPerAttemptTimeoutRecoversOnRetry(t *testing.T) {
	var attempts atomic.Int32
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if attempts.Add(1) == 1 {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       &contextDeadlineReadCloser{ctx: req.Context()},
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html>ok</html>")),
					Request:    req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	body, err := client.fetchPage(context.Background(), "https://www.youtube.com/test", FetchPolicy{
		MaxAttempts:       2,
		PerAttemptTimeout: 5 * time.Millisecond,
		BaseDelay:         time.Millisecond,
		MaxDelay:          time.Millisecond,
	})
	require.NoError(t, err)
	require.Equal(t, "<html>ok</html>", body)
	require.Equal(t, int32(2), attempts.Load())
}

type contextDeadlineReadCloser struct {
	ctx context.Context
}

func (r *contextDeadlineReadCloser) Read([]byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *contextDeadlineReadCloser) Close() error {
	return nil
}

func TestClassifyFailureNewResponseReasons(t *testing.T) {
	require.Equal(t, FailureReasonEmptyResponse, ClassifyFailure(ErrEmptyResponse, FailureSourceHTML).Reason)
	require.Equal(t, FailureReasonBlockedResponse, ClassifyFailure(ErrBlockedResponse, FailureSourceHTML).Reason)
	require.Equal(t, FailureReasonResponseTooLarge, ClassifyFailure(ErrResponseTooLarge, FailureSourceHTML).Reason)
	require.True(t, shouldRetryFetchPage(ErrEmptyResponse))
	require.False(t, shouldRetryFetchPage(ErrBlockedResponse))
	require.False(t, shouldRetryFetchPage(ErrResponseTooLarge))
	require.True(t, errors.Is(ErrBlockedResponse, ErrBlockedResponse))
}

func TestBodyLooksBlockedByYouTubeIgnoresGenericContentWords(t *testing.T) {
	normalBodies := []string{
		`{"title":"I solved the world's hardest CAPTCHA","videoId":"abc"}`,
		`{"description":"please enable cookies in your browser to save preferences"}`,
		`{"description":"Our systems have detected unusual traffic in this drama recap."}`,
		`{"comment":"before you continue to youtube, watch this clip"}`,
	}
	for _, body := range normalBodies {
		require.False(t, bodyLooksBlockedByYouTube([]byte(body)),
			"normal content mentioning generic phrases must not trigger fleet-wide cooldown: %s", body)
	}

	blockedBodies := []string{
		`<form action="https://www.google.com/recaptcha/api/challenge">`,
		`location.replace("https://www.youtube.com/sorry/index?continue=...")`,
		`<a href="https://consent.youtube.com/m">consent</a>`,
	}
	for _, body := range blockedBodies {
		require.True(t, bodyLooksBlockedByYouTube([]byte(body)),
			"real challenge page must stay classified blocked: %s", body)
	}
}

func TestResponseBodyReadErrorDoesNotMisclassifyTransportErrors(t *testing.T) {
	transportErr := errors.New("connection rate limit reached")
	got := responseBodyReadError(transportErr)
	require.Error(t, got)
	require.NotErrorIs(t, got, ErrResponseTooLarge,
		"transport error mentioning 'limit' must stay retryable, not become non-retryable too-large")
	require.ErrorIs(t, got, transportErr)

	typed := fmt.Errorf("read body: %w", jsonutil.ErrBodyTooLarge)
	require.ErrorIs(t, responseBodyReadError(typed), ErrResponseTooLarge)
}

func TestHasTransientTransportSignatureTreatsNXDomainAsPermanent(t *testing.T) {
	require.False(t, hasTransientTransportSignature("lookup www.youtube.com: no such host"),
		"NXDOMAIN is a permanent failure; retrying wastes budget within the attempt loop")
	require.True(t, hasTransientTransportSignature("lookup www.youtube.com: temporary failure in name resolution"))
}
