package scraping

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetShorts_DoesNotEagerlyEnrichPublishedAtFromRSS(t *testing.T) {
	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}},{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-2"}}},"overlayMetadata":{"primaryText":{"content":"Short Two"},"secondaryText":{"content":"2.3K views"}},"thumbnail":{"sources":[{"url":"https://img.test/2.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	rssCalls := 0

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: videosRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/shorts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsHTML)), Header: make(http.Header), Request: req}, nil
				case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
					rssCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("unexpected rss call")), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	shorts, err := client.GetShorts(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	require.Len(t, shorts, 2)
	assert.Zero(t, rssCalls)
	require.Nil(t, shorts[0].PublishedAt)
	require.Nil(t, shorts[1].PublishedAt)
}

func TestEnrichShortsPublishedAtFromRSS_FillsPublishedAt(t *testing.T) {
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <entry>
    <yt:videoId>short-1</yt:videoId>
    <title>Short One</title>
    <published>2026-04-10T01:11:12+00:00</published>
  </entry>
</feed>`
	shorts := []*Short{
		{VideoID: "short-1"},
		{VideoID: "short-2"},
	}

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: videosRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.True(t, strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(rssBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	client.EnrichShortsPublishedAtFromRSS(context.Background(), "UC_TEST", shorts)

	require.NotNil(t, shorts[0].PublishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", shorts[0].PublishedAt.Format(time.RFC3339Nano))
	assert.Nil(t, shorts[1].PublishedAt)
}
