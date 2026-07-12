package pollers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type videoFreshnessFixture struct {
	videoID       string
	publishedText string
}

type videoFreshnessRoutes struct {
	videosHTML string
	rssBody    string
	watch      func(videoID string) *http.Response
	rssCalls   int
	watchCalls map[string]int
}

func (r *videoFreshnessRoutes) roundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case strings.HasSuffix(req.URL.Path, "/videos"):
		return videoFreshnessResponse(req, http.StatusOK, r.videosHTML), nil
	case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
		r.rssCalls++
		return videoFreshnessResponse(req, http.StatusOK, r.rssBody), nil
	case req.URL.Path == "/watch":
		videoID := req.URL.Query().Get("v")
		r.watchCalls[videoID]++
		if r.watch != nil {
			response := r.watch(videoID)
			response.Request = req
			return response, nil
		}
		return videoFreshnessResponse(req, http.StatusNotFound, "not found"), nil
	default:
		return videoFreshnessResponse(req, http.StatusNotFound, "not found"), nil
	}
}

func videoFreshnessResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func videosFreshnessRSSFeed(items ...videoFreshnessFixture) string {
	var feed strings.Builder
	feed.WriteString(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">`)
	for _, item := range items {
		feed.WriteString(fmt.Sprintf(`<entry><yt:videoId>%s</yt:videoId><title>%s</title><published>%s</published></entry>`, item.videoID, item.videoID, item.publishedText))
	}
	feed.WriteString(`</feed>`)
	return feed.String()
}

func videosFreshnessTabHTML(t testing.TB, items ...videoFreshnessFixture) string {
	t.Helper()
	contents := make([]any, 0, len(items))
	for _, item := range items {
		contents = append(contents, map[string]any{
			"richItemRenderer": map[string]any{
				"content": map[string]any{
					"videoRenderer": map[string]any{
						"videoId":           item.videoID,
						"title":             map[string]any{"runs": []any{map[string]any{"text": item.videoID}}},
						"publishedTimeText": map[string]any{"simpleText": item.publishedText},
						"lengthText":        map[string]any{"simpleText": "10:00"},
					},
				},
			},
		})
	}
	initialData := map[string]any{
		"contents": map[string]any{
			"twoColumnBrowseResultsRenderer": map[string]any{
				"tabs": []any{map[string]any{
					"tabRenderer": map[string]any{
						"title": "Videos",
						"content": map[string]any{
							"richGridRenderer": map[string]any{"contents": contents},
						},
					},
				}},
			},
		},
	}
	raw, err := json.Marshal(initialData)
	require.NoError(t, err)
	return `<html><body><script>var ytInitialData = ` + string(raw) + `;</script></body></html>`
}

func TestVideosPollerMissingWatermarkStoresHistoricalPageWithoutNotifications(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_INCIDENT"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "gone-from-page",
	}).Error)

	seenAt := time.Now().UTC().Add(-5 * 365 * 24 * time.Hour)
	for _, videoID := range []string{"known-1", "known-2", "known-3", "known-4"} {
		require.NoError(t, db.Create(&domain.YouTubeVideo{
			VideoID:     videoID,
			ChannelID:   channelID,
			Title:       videoID,
			FirstSeenAt: seenAt,
			LastSeenAt:  seenAt,
		}).Error)
	}

	items := []videoFreshnessFixture{
		{videoID: "known-1", publishedText: "1y ago"},
		{videoID: "known-2", publishedText: "1y ago"},
		{videoID: "known-3", publishedText: "2y ago"},
		{videoID: "known-4", publishedText: "3y ago"},
		{videoID: "unseen-1", publishedText: "1y ago"},
		{videoID: "unseen-2", publishedText: "1y ago"},
		{videoID: "unseen-3", publishedText: "2y ago"},
		{videoID: "unseen-4", publishedText: "3y ago"},
		{videoID: "unseen-5", publishedText: "3y ago"},
		{videoID: "unseen-6", publishedText: "4y ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.EqualValues(t, 10, countRows(t, db, &domain.YouTubeVideo{}))
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "known-1", watermark.LastContentID)
}

func TestVideosPollerClassifiesRelativePublicationUnitsByFreshness(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_RELATIVE_UNITS"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "gone-from-page",
	}).Error)

	items := []videoFreshnessFixture{
		{videoID: "fresh-seconds", publishedText: "30 seconds ago"},
		{videoID: "fresh-minutes", publishedText: "15 mins ago"},
		{videoID: "fresh-hours", publishedText: "2 hours ago"},
		{videoID: "fresh-days", publishedText: "2 days ago"},
		{videoID: "old-days", publishedText: "4 days ago"},
		{videoID: "old-weeks", publishedText: "1 week ago"},
		{videoID: "old-months", publishedText: "1 month ago"},
		{videoID: "old-years", publishedText: "1 year ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	contentIDs := make([]string, 0, len(outboxRows))
	for i := range outboxRows {
		contentIDs = append(contentIDs, outboxRows[i].ContentID)
	}
	assert.ElementsMatch(t, []string{"fresh-seconds", "fresh-minutes", "fresh-hours", "fresh-days"}, contentIDs)
	assert.EqualValues(t, len(items), countRows(t, db, &domain.YouTubeVideo{}))
}

func TestVideosPollerMissingWatermarkNotifiesOnlyFreshVideoExactlyOnce(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_MIXED"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "gone-from-page",
	}).Error)

	items := []videoFreshnessFixture{
		{videoID: "historical", publishedText: "1 month ago"},
		{videoID: "genuine-new", publishedText: "2 hours ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "genuine-new", outboxRows[0].ContentID)
	assert.EqualValues(t, 2, countRows(t, db, &domain.YouTubeVideo{}))
}

func TestVideosPollerPartialReorderStoresHistoricalVideoWithoutNotification(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_REORDER"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	items := []videoFreshnessFixture{
		{videoID: "historical-reorder", publishedText: "3 weeks ago"},
		{videoID: "previous-head", publishedText: "1 day ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var stored domain.YouTubeVideo
	require.NoError(t, db.Where("video_id = ?", "historical-reorder").First(&stored).Error)
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "historical-reorder", watermark.LastContentID)
}

func TestVideosPollerDefersUnresolvedPublicationAndHoldsWatermark(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_DEFER"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	items := []videoFreshnessFixture{
		{videoID: "unresolved", publishedText: "publication unavailable"},
		{videoID: "previous-head", publishedText: "1 day ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var unresolvedCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Where("video_id = ?", "unresolved").Count(&unresolvedCount).Error)
	assert.Zero(t, unresolvedCount)
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "previous-head", watermark.LastContentID)
}

func TestVideosPollerResolvesUnclearHTMLPublicationFromRSSBeforeWatch(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_RSS_RESOLVE"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	freshPublishedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "rss-fresh", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody: videosFreshnessRSSFeed(videoFreshnessFixture{
			videoID:       "rss-fresh",
			publishedText: freshPublishedAt.Format(time.RFC3339),
		}),
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "rss-fresh", outboxRows[0].ContentID)
	assert.Equal(t, 1, routes.rssCalls)
	assert.Zero(t, routes.watchCalls["rss-fresh"])
	var stored domain.YouTubeVideo
	require.NoError(t, db.Where("video_id = ?", "rss-fresh").First(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.WithinDuration(t, freshPublishedAt, *stored.PublishedAt, time.Second)
}

func TestVideosPollerFallsBackToWatchWhenRSSHasNoPublication(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_WATCH_RESOLVE"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	freshPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "watch-fresh", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody: videosFreshnessRSSFeed(),
		watch: func(videoID string) *http.Response {
			require.Equal(t, "watch-fresh", videoID)
			return videoFreshnessResponse(nil, http.StatusOK,
				`<html><head><meta itemprop="uploadDate" content="`+freshPublishedAt.Format(time.RFC3339)+`"></head></html>`)
		},
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "watch-fresh", outboxRows[0].ContentID)
	assert.Equal(t, 1, routes.rssCalls)
	assert.Equal(t, 1, routes.watchCalls["watch-fresh"])
	var stored domain.YouTubeVideo
	require.NoError(t, db.Where("video_id = ?", "watch-fresh").First(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.WithinDuration(t, freshPublishedAt, *stored.PublishedAt, time.Second)
}

func TestVideosPollerUsesConclusiveHTMLRelativeTimeWithoutFallbackFetches(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_HTML_RELATIVE"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "html-fresh", publishedText: "2 hours ago"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody:    videosFreshnessRSSFeed(),
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "html-fresh", outboxRows[0].ContentID)
	assert.Zero(t, routes.rssCalls)
	assert.Zero(t, routes.watchCalls["html-fresh"])
}

func TestVideosPollerReevaluatesDeferredPublicationOnNextPoll(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_REEVALUATE"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	freshPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "flaky-fresh", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody:    videosFreshnessRSSFeed(),
		watchCalls: make(map[string]int),
	}
	routes.watch = func(videoID string) *http.Response {
		require.Equal(t, "flaky-fresh", videoID)
		if routes.watchCalls[videoID] == 1 {
			return videoFreshnessResponse(nil, http.StatusOK, `<html><head></head></html>`)
		}
		return videoFreshnessResponse(nil, http.StatusOK,
			`<html><head><meta itemprop="uploadDate" content="`+freshPublishedAt.Format(time.RFC3339)+`"></head></html>`)
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "previous-head", watermark.LastContentID)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "flaky-fresh", outboxRows[0].ContentID)
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "flaky-fresh", watermark.LastContentID)
	assert.Equal(t, 2, routes.watchCalls["flaky-fresh"])
}

func TestVideosPollerAbsorbsUnresolvedPublicationSilentlyAfterBoundedAttempts(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_ABSORB"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "dateless", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody: videosFreshnessRSSFeed(),
		watch: func(videoID string) *http.Response {
			require.Equal(t, "dateless", videoID)
			return videoFreshnessResponse(nil, http.StatusOK, `<html><head></head></html>`)
		},
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	require.NoError(t, poller.Poll(context.Background(), channelID))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeVideo{}))

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var stored domain.YouTubeVideo
	require.NoError(t, db.Where("video_id = ?", "dateless").First(&stored).Error)
	assert.Nil(t, stored.PublishedAt)
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "dateless", watermark.LastContentID)
	assert.Equal(t, 3, routes.watchCalls["dateless"])
}

func TestVideosPollerKnownVideoSkipsFreshnessResolveAndPreservesFailedOutbox(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_KNOWN_FAILED"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)
	seenAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "known-failed",
		ChannelID:   channelID,
		Title:       "known-failed",
		FirstSeenAt: seenAt,
		LastSeenAt:  seenAt,
	}).Error)
	failedOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     channelID,
		ContentID:     "known-failed",
		Payload:       `{"video_id":"known-failed","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: seenAt,
		CreatedAt:     seenAt,
		Error:         "delivery exhausted",
	}
	require.NoError(t, db.Create(&failedOutbox).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID:      failedOutbox.ID,
		RoomID:        "room-1",
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: seenAt,
		CreatedAt:     seenAt,
		Error:         "delivery exhausted",
	}).Error)

	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "known-failed", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody: videosFreshnessRSSFeed(),
		watch: func(videoID string) *http.Response {
			return videoFreshnessResponse(nil, http.StatusOK, `<html><head></head></html>`)
		},
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.Zero(t, routes.rssCalls)
	assert.Zero(t, routes.watchCalls["known-failed"])
	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.Where("kind = ? AND content_id = ?", domain.OutboxKindNewVideo, "known-failed").First(&outbox).Error)
	assert.Equal(t, domain.OutboxStatusFailed, outbox.Status)
	assert.Equal(t, 3, outbox.AttemptCount)
	assert.Contains(t, outbox.Payload, `"version":"old"`)
	var delivery domain.YouTubeNotificationDelivery
	require.NoError(t, db.Where("outbox_id = ?", failedOutbox.ID).First(&delivery).Error)
	assert.Equal(t, domain.OutboxStatusFailed, delivery.Status)
	assert.Equal(t, 3, delivery.AttemptCount)
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "known-failed", watermark.LastContentID)
}

func TestVideosPollerInitialBaselineStoresWithoutFreshnessFetchOrNotification(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_BASELINE"
	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "baseline-unknown", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "baseline-old", publishedText: "2 years ago"},
		),
		rssBody: videosFreshnessRSSFeed(),
		watch: func(videoID string) *http.Response {
			return videoFreshnessResponse(nil, http.StatusOK, `<html><head></head></html>`)
		},
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))

	assert.EqualValues(t, 2, countRows(t, db, &domain.YouTubeVideo{}))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.Zero(t, routes.rssCalls)
	assert.Empty(t, routes.watchCalls)
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "baseline-unknown", watermark.LastContentID)
}

func TestVideosPollerDuplicatePollEnqueuesFreshVideoExactlyOnce(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_DUPLICATE"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)
	items := []videoFreshnessFixture{
		{videoID: "fresh-duplicate", publishedText: "1 hour ago"},
		{videoID: "previous-head", publishedText: "1 day ago"},
	}
	poller := NewVideosPoller(newVideosTestClient(t, videosPageTransport(videosFreshnessTabHTML(t, items...))), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	require.NoError(t, db.Model(&domain.YouTubeContentWatermark{}).
		Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).
		Update("last_content_id", "previous-head").Error)
	require.NoError(t, poller.Poll(context.Background(), channelID))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "fresh-duplicate", outboxRows[0].ContentID)
}

func TestVideosPollerHoldsWatermarkWhileDeferredVideoIsMissingFromPage(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	channelID := "UC_VIDEO_DEPARTED"
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "previous-head",
	}).Error)

	routes := &videoFreshnessRoutes{
		videosHTML: videosFreshnessTabHTML(t,
			videoFreshnessFixture{videoID: "departed-unresolved", publishedText: "publication unavailable"},
			videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
		),
		rssBody: videosFreshnessRSSFeed(),
		watch: func(videoID string) *http.Response {
			return videoFreshnessResponse(nil, http.StatusOK, `<html><head></head></html>`)
		},
		watchCalls: make(map[string]int),
	}
	poller := NewVideosPoller(newVideosTestClient(t, shortsPollerRoundTripFunc(routes.roundTrip)), db, 10)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	routes.videosHTML = videosFreshnessTabHTML(t,
		videoFreshnessFixture{videoID: "other-fresh", publishedText: "1 hour ago"},
		videoFreshnessFixture{videoID: "previous-head", publishedText: "1 day ago"},
	)
	require.NoError(t, poller.Poll(context.Background(), channelID))

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "previous-head", watermark.LastContentID)
	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "other-fresh", outboxRows[0].ContentID)

	require.NoError(t, poller.Poll(context.Background(), channelID))
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeVideo).First(&watermark).Error)
	assert.Equal(t, "other-fresh", watermark.LastContentID)
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
}
