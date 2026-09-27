package scraping

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	ratelimiter "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

// 테스트 Client는 운영 기본값과 같은 YouTube 설정으로 만든다.
func testYouTubeConfig() settings.YouTubeConfig {
	return settings.DefaultYouTubeOperationalConfig()
}

type bucketRecordingLimiter struct {
	mu      sync.Mutex
	buckets []string
}

func (l *bucketRecordingLimiter) Allow(_ context.Context, bucket string, _ int, _ time.Duration) (ratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buckets = append(l.buckets, bucket)

	return ratelimit.Decision{Allowed: true}, nil
}

// runtime 설정(YOUTUBE_SCRAPER_*_TIMEOUT_SECONDS, YOUTUBE_*_TTL_SECONDS)이 Client에 그대로 들어가야 한다.
// 패키지 기본값을 읽으면 운영 설정이 조용히 무시된다(stack audit A3).
func TestNewClientUsesInjectedYouTubeConfig(t *testing.T) {
	cfg := testYouTubeConfig()

	cfg.ScraperHTTPTimeout = 7 * time.Second
	cfg.ScraperDialTimeout = 2 * time.Second
	cfg.ScraperHeaderTimeout = 3 * time.Second
	cfg.CommunityMissingTTL = 11 * time.Minute

	client := NewClient(cfg)

	require.NotNil(t, client.httpClient)
	require.NotNil(t, client.transport)
	assert.Equal(t, 7*time.Second, client.httpClient.Timeout)
	assert.Equal(t, 2*time.Second, client.transport.TLSHandshakeTimeout)
	assert.Equal(t, 3*time.Second, client.transport.ResponseHeaderTimeout)
	assert.Equal(t, 11*time.Minute, client.communityMissing.ttl)
}

func TestFetchPagePreflightUsesInjectedDistributedBucketBase(t *testing.T) {
	cfg := testYouTubeConfig()

	cfg.DistributedRateLimit.BucketBase = "youtube:configured"

	recorder := &bucketRecordingLimiter{}
	limiter := ratelimiter.New(0)
	require.NoError(t, limiter.ConfigureDistributed(recorder, 1, time.Second))

	client := NewClient(cfg, WithRateLimiter(limiter))

	require.NoError(t, client.fetchPagePreflight(t.Context(), "https://www.youtube.com/channel/UC1/videos"))
	assert.Equal(t, []string{"youtube:configured:channel:UC1:videos"}, recorder.buckets)
}

func TestNetHTTPPageFetcherUsesInjectedMaxPageBodyBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, strings.Repeat("x", 64))
	}))
	defer server.Close()

	cfg := testYouTubeConfig()

	cfg.MaxPageBodyBytes = 16

	client := NewClient(cfg, WithHTTPClient(server.Client()))

	_, err := netHTTPPageFetcher{client: client}.FetchPage(t.Context(), pageFetchRequest{URL: server.URL})
	require.ErrorIs(t, err, ErrResponseTooLarge)
}

// 기본 FetchPolicy의 시도별 timeout은 주입된 ScraperHTTPTimeout을 따라야 한다. 코드 기본값(15s)으로
// 시도를 자르면 YOUTUBE_SCRAPER_HTTP_TIMEOUT_SECONDS를 올린 설정이 조용히 무시된다(stack audit A3).
func TestFetchPageAttemptTimeoutFollowsInjectedScraperHTTPTimeout(t *testing.T) {
	const configuredTimeout = 30 * time.Second

	cases := map[string]struct {
		policy FetchPolicy
		want   time.Duration
	}{
		"default":        {policy: DefaultFetchPolicy, want: configuredTimeout},
		"high frequency": {policy: HighFrequencyChannelFetchPolicy, want: configuredTimeout},
		// 정적 상한을 명시한 정책은 그 값을 유지한다.
		"metadata static override": {policy: MetadataResolveFetchPolicy, want: 10 * time.Second},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testYouTubeConfig()

			cfg.ScraperHTTPTimeout = configuredTimeout

			var (
				remaining   time.Duration
				hasDeadline bool
			)

			transport := videosRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var deadline time.Time

				deadline, hasDeadline = req.Context().Deadline()
				remaining = time.Until(deadline)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("<html>ytInitialData = {};</html>")),
					Request:    req,
				}, nil
			})
			client := NewClient(cfg, WithHTTPClient(&http.Client{Transport: transport}))

			_, err := client.fetchPage(t.Context(), "https://www.youtube.com/channel/UC1/videos", tc.policy)
			require.NoError(t, err)
			require.True(t, hasDeadline, "fetch attempt context has no deadline")
			assert.InDelta(t, tc.want.Seconds(), remaining.Seconds(), 1)
		})
	}
}
