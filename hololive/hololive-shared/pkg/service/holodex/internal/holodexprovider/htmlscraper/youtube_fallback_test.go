package htmlscraper

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type fallbackRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f fallbackRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchFromYouTubeProducerKeepsInjectedFetcherBehavior(t *testing.T) {
	called := 0
	service := NewTestServiceWithHTTPClient(nil, slog.Default(), "", func(context.Context, string) ([]*scraper.UpcomingEvent, error) {
		called++
		return []*scraper.UpcomingEvent{{VideoID: "video", Title: "title", Status: "LIVE"}}, nil
	})

	streams, err := service.FetchFromYouTubeProducer(context.Background(), "UCtest")

	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, domain.StreamStatusLive, streams[0].Status)
	require.Equal(t, 1, called)
}

func TestFetchFromYouTubeProducerWaitAdmissionUsesInjectedFetcherInTests(t *testing.T) {
	called := 0
	service := NewTestServiceWithHTTPClient(nil, slog.Default(), "", func(context.Context, string) ([]*scraper.UpcomingEvent, error) {
		called++
		return nil, nil
	})

	_, err := service.FetchFromYouTubeProducerWaitAdmission(context.Background(), "UCtest")

	require.NoError(t, err)
	require.Equal(t, 1, called)
}

func TestFetchFromYouTubeProducerWaitAdmissionUsesScraperBlockingAdmission(t *testing.T) {
	limiter := scraper.NewRateLimiter(25 * time.Millisecond)
	decision, err := limiter.TryReserve(context.Background())
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	var requests atomic.Int32
	client := scraper.NewClient(
		scraper.WithRateLimiter(limiter),
		scraper.WithHTTPClient(&http.Client{
			Transport: fallbackRoundTripFunc(func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html></html>")),
				}, nil
			}),
		}),
	)
	service := NewServiceWithYouTubeProducer(nil, nil, client, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = service.FetchFromYouTubeProducerWaitAdmission(ctx, "UCtest")

	require.Error(t, err)
	require.False(t, scraper.IsAdmissionDeferred(err), "wait admission should not return a non-blocking admission deferral")
	require.Equal(t, int32(1), requests.Load())
	require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond)
}
