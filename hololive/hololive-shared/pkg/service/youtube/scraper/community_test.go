package scraper

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

type communityRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f communityRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetCommunityPosts_404TreatAsEmpty(t *testing.T) {
	var attempts atomic.Int32

	httpClient := &http.Client{
		Transport: communityRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	client := NewClient(
		WithHTTPClient(httpClient),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	posts, err := client.GetCommunityPosts(context.Background(), "UC_TEST", 5)
	require.NoError(t, err)
	require.Empty(t, posts)
	require.Equal(t, int32(1), attempts.Load())

	posts, err = client.GetCommunityPosts(context.Background(), "UC_TEST", 5)
	require.NoError(t, err)
	require.Empty(t, posts)
	require.Equal(t, int32(1), attempts.Load(), "community missing cache should skip second network call")
}
