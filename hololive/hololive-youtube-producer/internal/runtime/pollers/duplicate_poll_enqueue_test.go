package pollers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"

	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/stretchr/testify/require"
)

func TestCommunityPollerDuplicatePollEnqueuesExactlyOnce(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	const (
		channelID   = "UC_DUPLICATE_POLL_COMMUNITY"
		postID      = "community:post-duplicate-poll"
		lastContent = "old-post"
	)

	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: lastContent,
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-duplicate-poll","authorEndpoint":{"browseEndpoint":{"browseId":"UC_DUPLICATE_POLL_COMMUNITY"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"duplicate poll community"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}]}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"

	client := scraper.NewClient(
		scraper.WithRateLimiter(ratelimiter.New(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewCommunityPoller(client, db, 10, nil)

	persistCommunityFromClient(t, poller, channelID)
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindCommunityPost, postID)

	rewindDuplicatePollWatermark(t, db, channelID, domain.WatermarkTypeCommunityPost, lastContent)

	persistCommunityFromClient(t, poller, channelID)
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindCommunityPost, postID)

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	require.EqualValues(t, 1, postCount)
}

func TestShortsPollerDuplicatePollEnqueuesExactlyOnce(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)

	const (
		channelID   = "UC_DUPLICATE_POLL_SHORTS"
		postID      = "short:short-duplicate-poll"
		lastContent = "old-short"
	)

	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: lastContent,
	}).Error)

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-duplicate-poll"}}},"overlayMetadata":{"primaryText":{"content":"Short Duplicate Poll"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}},{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"old-short"}}},"overlayMetadata":{"primaryText":{"content":"Old Short"},"secondaryText":{"content":"900 views"}},"thumbnail":{"sources":[{"url":"https://img.test/old.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
</feed>`
	watchHTML := `
		<html>
			<head>
				<meta itemprop="uploadDate" content="` + time.Now().UTC().Add(-time.Hour).Format(time.RFC3339) + `">
			</head>
		</html>
	`

	client := scraper.NewClient(
		scraper.WithRateLimiter(ratelimiter.New(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/shorts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsHTML)), Header: make(http.Header), Request: req}, nil
				case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(rssBody)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/watch" && req.URL.Query().Get("v") == "short-duplicate-poll":
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10)
	ctx := context.Background()

	require.NoError(t, poller.Poll(ctx, channelID))
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindNewShort, postID)

	rewindDuplicatePollWatermark(t, db, channelID, domain.WatermarkTypeShort, lastContent)

	require.NoError(t, poller.Poll(ctx, channelID))
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindNewShort, postID)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.EqualValues(t, 1, videoCount)
}

func rewindDuplicatePollWatermark(t *testing.T, db *pollerBatchTestDB, channelID string, watermarkType domain.WatermarkType, lastContentID string) {
	t.Helper()

	result := db.Model(&domain.YouTubeContentWatermark{}).
		Where("channel_id = ? AND watermark_type = ?", channelID, watermarkType).
		Updates(map[string]any{
			"initialized":     true,
			"last_content_id": lastContentID,
		})
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected)
}

func requireDuplicatePollSingleEnqueuedState(t *testing.T, db *pollerBatchTestDB, kind domain.OutboxKind, canonicalPostID string) {
	t.Helper()

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Where("kind = ?", kind).Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, canonicalPostID, outboxRows[0].ContentID)
	require.Equal(t, domain.OutboxStatusPending, outboxRows[0].Status)
	require.Nil(t, outboxRows[0].SentAt)

	var trackingRow domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Where("kind = ? AND content_id = ?", kind, canonicalPostID).First(&trackingRow).Error)
	require.Nil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, trackingRow.DeliveryStatus)

	var stateRow domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.Where("kind = ? AND post_id = ?", kind, canonicalPostID).First(&stateRow).Error)
	require.Equal(t, canonicalPostID, stateRow.PostID)
	require.Equal(t, canonicalPostID, stateRow.ContentID)
	require.Nil(t, stateRow.AlarmSentAt)
	require.NotNil(t, stateRow.ActualPublishedAt, "enqueued alarms must carry the resolved published_at")
	require.Nil(t, stateRow.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, stateRow.DeliveryStatus)
}
