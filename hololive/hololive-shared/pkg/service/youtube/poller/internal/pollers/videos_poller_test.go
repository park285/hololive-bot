package pollers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

// videosTabHTML: /videos 경로로 제공하는 최소 YouTube 응답 HTML
// richGridRenderer → richItemRenderer → videoRenderer 구조
const videosTabHTML = `<html><body><script>var ytInitialData = {"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"videoRenderer":{"videoId":"vid-new","title":{"runs":[{"text":"New Video"}]},"publishedTimeText":{"simpleText":"2 hours ago"},"lengthText":{"simpleText":"10:00"},"viewCountText":{"simpleText":"1,000 views"}}}}},{"richItemRenderer":{"content":{"videoRenderer":{"videoId":"vid-old","title":{"runs":[{"text":"Old Video"}]},"publishedTimeText":{"simpleText":"1 month ago"},"lengthText":{"simpleText":"8:30"},"viewCountText":{"simpleText":"50,000 views"}}}}}]}}}}]}}}}</script></body></html>`

const emptyVideosTabHTML = `<html><body><script>var ytInitialData = {"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos","content":{"richGridRenderer":{"contents":[]}}}}]}}}</script></body></html>`

func newVideosTestClient(t *testing.T, transport http.RoundTripper) *scraper.Client {
	t.Helper()
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{Transport: transport}),
	)
}

func videosPageTransport(html string) shortsPollerRoundTripFunc {
	return shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/videos"):
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(html)), Header: make(http.Header), Request: req}, nil
		default:
			// RSS 폴백 등 다른 경로는 빈 피드로 응답
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
		}
	})
}

func TestVideosPollerInitializesWatermark(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	client := newVideosTestClient(t, videosPageTransport(videosTabHTML))
	poller := NewVideosPoller(client, db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_VID"))

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "UC_VID", domain.WatermarkTypeVideo).First(&watermark).Error)
	require.True(t, watermark.Initialized)
	require.Equal(t, "vid-new", watermark.LastContentID)
}

func TestVideosPollerDoesNotNotifyOnFirstPoll(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	client := newVideosTestClient(t, videosPageTransport(videosTabHTML))
	poller := NewVideosPoller(client, db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_VID2"))

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestVideosPollerNotifiesOnNewVideoAfterBaseline(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	// baseline 설정: watermark를 기존 비디오로 초기화
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_VID3",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "vid-old",
	}).Error)

	client := newVideosTestClient(t, videosPageTransport(videosTabHTML))
	poller := NewVideosPoller(client, db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_VID3"))

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Where("kind = ?", domain.OutboxKindNewVideo).Count(&outboxCount).Error)
	require.Equal(t, int64(1), outboxCount)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.Where("kind = ? AND content_id = ?", domain.OutboxKindNewVideo, "vid-new").First(&outbox).Error)
	require.Equal(t, domain.OutboxStatusPending, outbox.Status)
}

func TestVideosPollerHandlesEmptyVideoList(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	client := newVideosTestClient(t, videosPageTransport(emptyVideosTabHTML))
	poller := NewVideosPoller(client, db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_VID4"))

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.Zero(t, videoCount)
}

func TestVideosPollerHandlesScraperError(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	scraperErr := errors.New("transport failed")
	client := newVideosTestClient(t, shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, scraperErr
	}))
	poller := NewVideosPoller(client, db, 10)

	err := poller.Poll(context.Background(), "UC_VID5")
	require.Error(t, err)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.Zero(t, videoCount)
}

func TestVideosPollerSkipsLiveReplayVideos(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	// baseline 있음 + 신규 비디오가 live replay (publishedText에 "Streamed" 포함)
	const liveReplayHTML = `<html><body><script>var ytInitialData = {"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"videoRenderer":{"videoId":"live-replay","title":{"runs":[{"text":"Live Stream Replay"}]},"publishedTimeText":{"simpleText":"Streamed 2 hours ago"},"lengthText":{"simpleText":"2:00:00"}}}}}]}}}}]}}}};</script></body></html>`

	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_VID6",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "prev-vid",
	}).Error)

	client := newVideosTestClient(t, videosPageTransport(liveReplayHTML))
	poller := NewVideosPoller(client, db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_VID6"))

	// live replay는 알림 outbox에 추가하지 않는다
	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)

	// 비디오 행은 저장된다
	var video domain.YouTubeVideo
	require.NoError(t, db.Where("video_id = ?", "live-replay").First(&video).Error)
	require.True(t, video.IsLiveReplay)
}

func TestVideosPollerName(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	client := newVideosTestClient(t, videosPageTransport(emptyVideosTabHTML))
	poller := NewVideosPoller(client, db, 10)
	require.Equal(t, "videos", poller.Name())
}

func TestVideosPollerDefaultMaxResults(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	client := newVideosTestClient(t, videosPageTransport(emptyVideosTabHTML))
	// maxResults <= 0이면 기본값 10으로 초기화한다
	poller := NewVideosPoller(client, db, 0)
	require.Equal(t, "videos", poller.Name())
}
