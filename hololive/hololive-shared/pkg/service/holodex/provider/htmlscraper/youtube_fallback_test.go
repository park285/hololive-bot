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
	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/admission"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type fallbackRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f fallbackRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchYouTubeScheduleKeepsInjectedFetcherBehavior(t *testing.T) {
	called := 0
	service := newTestServiceWithHTTPClient(nil, slog.Default(), "", func(context.Context, string) ([]*parser.UpcomingEvent, error) {
		called++
		return []*parser.UpcomingEvent{{VideoID: "video", Title: "title", Status: "LIVE"}}, nil
	})

	streams, err := service.FetchYouTubeSchedule(context.Background(), "UCtest")

	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, domain.StreamStatusLive, streams[0].Status)
	require.Equal(t, 1, called)
}

func TestFetchYouTubeScheduleWaitAdmissionUsesInjectedFetcherInTests(t *testing.T) {
	called := 0
	service := newTestServiceWithHTTPClient(nil, slog.Default(), "", func(context.Context, string) ([]*parser.UpcomingEvent, error) {
		called++
		return nil, nil
	})

	_, err := service.FetchYouTubeScheduleWaitAdmission(context.Background(), "UCtest")

	require.NoError(t, err)
	require.Equal(t, 1, called)
}

func TestFetchYouTubeScheduleWaitAdmissionUsesScraperBlockingAdmission(t *testing.T) {
	limiter := ratelimiter.New(25 * time.Millisecond)
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
	service := NewServiceWithYouTubeClient(nil, nil, client, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = service.FetchYouTubeScheduleWaitAdmission(ctx, "UCtest")

	require.Error(t, err)
	require.False(t, youtubeadmission.IsDeferred(err), "wait admission should not return a non-blocking admission deferral")
	require.Equal(t, int32(1), requests.Load())
	require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond)
}
