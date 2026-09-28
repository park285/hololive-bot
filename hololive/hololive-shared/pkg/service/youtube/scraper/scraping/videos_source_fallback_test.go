package scraping

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	ratelimiter "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

const recentVideosSourceFallbackRSS = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <yt:videoId>rss-video-1</yt:videoId>
    <title>From RSS</title>
    <published>2026-09-26T00:00:00+00:00</published>
  </entry>
</feed>`

// DEC-20260926-hololive-source-fallbacks-retirement: GetRecentVideos는 /videos HTML 하나만 원천으로 쓴다.
// HTML 실패를 RSS로 보충하거나, RSS까지 비었을 때 빈 성공으로 돌려주지 않는다.
func TestGetRecentVideos_ReturnsHTMLErrorWithoutRSSFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rssBody string
	}{
		{name: "rss has videos", rssBody: recentVideosSourceFallbackRSS},
		{name: "rss is empty", rssBody: `<feed xmlns="http://www.w3.org/2005/Atom"></feed>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				videosPageCalls atomic.Int32
				rssCalls        atomic.Int32
			)

			client := NewClient(testYouTubeConfig(),
				WithRateLimiter(ratelimiter.New(0)),
				WithHTTPClient(&http.Client{
					Timeout: 5 * time.Second,
					Transport: videosRoundTripFunc(func(req *http.Request) (*http.Response, error) {
						status, body := http.StatusNotFound, "not found"

						switch {
						case strings.HasSuffix(req.URL.Path, "/videos"):
							videosPageCalls.Add(1)

							// ytInitialData가 없는 HTML은 parser drift로 분류돼 예전에는 RSS 보충을 탔다.
							status, body = http.StatusOK, "<html><body>missing initial data</body></html>"
						case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
							rssCalls.Add(1)

							status, body = http.StatusOK, tc.rssBody
						}

						return &http.Response{
							StatusCode: status,
							Body:       io.NopCloser(strings.NewReader(body)),
							Header:     make(http.Header),
							Request:    req,
						}, nil
					}),
				}),
			)

			videos, err := client.GetRecentVideos(t.Context(), "UC_TEST", 10)
			require.Error(t, err, "videos = %#v", videos)
			require.ErrorIs(t, err, parser.ErrParserDrift)
			require.Nil(t, videos)
			assert.Equal(t, int32(1), videosPageCalls.Load())
			assert.Equal(t, int32(0), rssCalls.Load())
		})
	}
}
