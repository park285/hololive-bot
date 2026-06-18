// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scraping

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videosRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f videosRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseVideosFromInitialData_Normal(t *testing.T) {
	jsonBytes, err := os.ReadFile("testdata/videos_tab_pekora.json")
	require.NoError(t, err, "Fixture 파일 읽기 실패")

	data := gjson.ParseBytes(jsonBytes)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "UC1DCedRgGHBdm81E1llLhOQ",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(videos), 1, "최소 1개 비디오가 있어야 함")

	if len(videos) > 0 {
		assert.NotEmpty(t, videos[0].VideoID)
		assert.NotEmpty(t, videos[0].Title)
	}
}

func TestParseVideosFromInitialData_Empty(t *testing.T) {
	jsonBytes, err := os.ReadFile("testdata/videos_tab_empty.json")
	require.NoError(t, err, "Fixture 파일 읽기 실패")

	data := gjson.ParseBytes(jsonBytes)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err, "Videos 탭이 있으면 비디오 0개여도 에러 아님")
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_NoTab(t *testing.T) {
	jsonStr := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"홈"}}]}}}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err, "Videos 탭 없음 = 스트리밍 전용 채널, 에러 아님")
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_NoTabsStructure(t *testing.T) {
	jsonStr := `{"contents":{"someOtherRenderer":{}}}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err, "tabs 구조가 없어도 전체 폴링 실패로 처리하지 않음")
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_PartialInitialData(t *testing.T) {
	jsonStr := `{"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"test"}}}}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err, "contents/alerts 없는 부분 응답은 빈 결과로 처리")
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_FallbackExtractVideoRenderer(t *testing.T) {
	jsonStr := `{
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"customContainer": {
					"items": [
						{
							"richItemRenderer": {
								"content": {
									"videoRenderer": {
										"videoId": "fallback123",
										"title": {"runs": [{"text": "Fallback Video"}]}
									}
								}
							}
						}
					]
				}
			}
		}
	}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "fallback123", videos[0].VideoID)
	assert.Equal(t, "Fallback Video", videos[0].Title)
}

func TestParseVideosFromInitialData_EndpointDetection(t *testing.T) {
	jsonStr := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[
		{"tabRenderer":{"title":"Unknown","endpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/@channel/videos"}}},"content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"videoRenderer":{"videoId":"abc123","title":{"runs":[{"text":"Test Video"}]}}}}}]}}}}
	]}}}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.NoError(t, err)
	assert.Len(t, videos, 1, "endpoint URL로 Videos 탭 탐지")
	assert.Equal(t, "abc123", videos[0].VideoID)
}

func TestParseVideosFromInitialData_ChannelNotExist(t *testing.T) {
	jsonStr := `{"alerts":[{"alertRenderer":{"type":"ERROR","text":{"simpleText":"This channel does not exist."}}}]}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.Error(t, err)
	assert.Nil(t, videos)
	assert.True(t, errors.Is(err, ErrChannelNotFound))
}

func TestParseVideosFromInitialData_ChannelTerminated(t *testing.T) {
	jsonStr := `{"alerts":[{"alertRenderer":{"type":"ERROR","text":{"simpleText":"This channel has been terminated."}}}]}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.Error(t, err)
	assert.Nil(t, videos)
	assert.True(t, errors.Is(err, ErrChannelNotFound))
}

func TestParseVideosFromInitialData_ChannelNotFoundInSecondErrorAlert(t *testing.T) {
	jsonStr := `{"alerts":[
		{"alertRenderer":{"type":"ERROR","text":{"simpleText":"Temporarily unavailable."}}},
		{"alertRenderer":{"type":"ERROR","text":{"simpleText":"This channel does not exist."}}}
	]}`
	data := gjson.Parse(jsonStr)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "test-channel",
		10,
		client.parseVideoRenderer,
	)

	require.Error(t, err)
	assert.Nil(t, videos)
	assert.True(t, errors.Is(err, ErrChannelNotFound))
}

func TestCheckAlerts_ExtractsRunsText(t *testing.T) {
	jsonStr := `{"alerts":[
		{"alertRenderer":{"type":"ERROR","text":{"runs":[{"text":"This channel "},{"text":"has been terminated."}]}}}
	]}`
	data := gjson.Parse(jsonStr)

	err := checkAlerts(&data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelNotFound))
	assert.Contains(t, err.Error(), "has been terminated")
}

func TestParseVideosFromRSSFeed(t *testing.T) {
	rssXML := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <entry>
    <yt:videoId>vid001</yt:videoId>
    <title>First Video</title>
    <published>2026-02-18T12:34:56+00:00</published>
    <author><name>Test Channel</name></author>
    <media:group>
      <media:thumbnail url="https://i.ytimg.com/vi/vid001/hqdefault.jpg" width="480" height="360" />
    </media:group>
  </entry>
  <entry>
    <yt:videoId>vid002</yt:videoId>
    <title>Second Video</title>
    <published>2026-02-17T11:22:33+00:00</published>
    <author><name>Test Channel</name></author>
  </entry>
</feed>`

	videos, err := parseVideosFromRSSFeed(rssXML, "UC_TEST", 10)
	require.NoError(t, err)
	require.Len(t, videos, 2)

	assert.Equal(t, "vid001", videos[0].VideoID)
	assert.Equal(t, "First Video", videos[0].Title)
	assert.Equal(t, "UC_TEST", videos[0].ChannelID)
	assert.Equal(t, "Test Channel", videos[0].ChannelTitle)
	assert.Equal(t, "2026-02-18T12:34:56Z", videos[0].PublishedText)
	require.NotEmpty(t, videos[0].Thumbnail)
	assert.Equal(t, "https://i.ytimg.com/vi/vid001/hqdefault.jpg", videos[0].Thumbnail[0].URL)
}

func TestParseVideosFromRSSFeed_MaxResultsAndInvalidEntries(t *testing.T) {
	rssXML := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015">
  <entry>
    <yt:videoId>vid001</yt:videoId>
    <title>First Video</title>
    <published>2026-02-18T12:34:56+00:00</published>
  </entry>
  <entry>
    <yt:videoId></yt:videoId>
    <title>Invalid Missing ID</title>
  </entry>
  <entry>
    <yt:videoId>vid001</yt:videoId>
    <title>Duplicate Video</title>
  </entry>
  <entry>
    <yt:videoId>vid002</yt:videoId>
    <title>Second Video</title>
    <published>2026-02-17T11:22:33+00:00</published>
  </entry>
</feed>`

	videos, err := parseVideosFromRSSFeed(rssXML, "UC_TEST", 1)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "vid001", videos[0].VideoID)
}

func TestParseVideosFromInitialData_LockupViewModel(t *testing.T) {
	data := gjson.Parse(`{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"title": "動画",
						"endpoint": {"commandMetadata": {"webCommandMetadata": {"url": "/@example/videos"}}},
						"content": {
							"richGridRenderer": {
								"contents": [{
									"richItemRenderer": {
										"content": {
											"lockupViewModel": {
												"contentId": "lockup001",
												"contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
												"contentImage": {
													"thumbnailViewModel": {
														"image": {"sources": [{"url": "https://i.ytimg.com/vi/lockup001/hqdefault.jpg", "width": 336, "height": 188}]},
														"overlays": [{
															"thumbnailBottomOverlayViewModel": {
																"badges": [{
																	"thumbnailBadgeViewModel": {"text": "4:23"}
																}]
															}
														}]
													}
												},
												"metadata": {
													"lockupMetadataViewModel": {
														"title": {"content": "Lockup Video"},
														"metadata": {
															"contentMetadataViewModel": {
																"metadataRows": [{
																	"metadataParts": [
																		{"text": {"content": "69万回視聴"}},
																		{"text": {"content": "1 month ago"}}
																	]
																}]
															}
														}
													}
												}
											}
										}
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	client := NewClient()
	videos, err := parseVideosFromInitialData(&data, "UC_TEST", 10, client.parseVideoRenderer)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "lockup001", videos[0].VideoID)
	assert.Equal(t, "Lockup Video", videos[0].Title)
	assert.Equal(t, "4:23", videos[0].Duration)
	assert.Equal(t, "1 month ago", videos[0].PublishedText)
	assert.Equal(t, int64(690000), videos[0].ViewCount)
	assert.Len(t, videos[0].Thumbnail, 1)
}

func TestGetRecentVideos_NoRSSFallbackOnEmptySuccess(t *testing.T) {
	jsonBytes, err := os.ReadFile("testdata/videos_tab_empty.json")
	require.NoError(t, err)

	htmlBody := "<script>var ytInitialData = " + string(jsonBytes) + ";</script>"
	rssBody := `<?xml version="1.0" encoding="UTF-8"?><feed></feed>`

	var videosPageCalls int32
	var rssCalls int32

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: videosRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				path := req.URL.Path
				if strings.HasSuffix(path, "/videos") {
					atomic.AddInt32(&videosPageCalls, 1)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(htmlBody)),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				}
				if strings.HasSuffix(path, "/feeds/videos.xml") {
					atomic.AddInt32(&rssCalls, 1)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(rssBody)),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				}

				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	videos, err := client.GetRecentVideos(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	require.Len(t, videos, 0)
	assert.Equal(t, int32(1), atomic.LoadInt32(&videosPageCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&rssCalls))
}
