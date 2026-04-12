package poller

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

type duplicatePollTestSender struct {
	mu       sync.Mutex
	messages []duplicatePollSentMessage
}

type duplicatePollSentMessage struct {
	roomID  string
	message string
}

func (s *duplicatePollTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, duplicatePollSentMessage{
		roomID:  roomID,
		message: message,
	})
	return nil
}

func (s *duplicatePollTestSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func TestCommunityPollerDuplicatePollDispatchesExactlyOnce(t *testing.T) {
	db := newBatchTestDB(t,
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

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-duplicate-poll","authorEndpoint":{"browseEndpoint":{"browseId":"UC_DUPLICATE_POLL_COMMUNITY"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"duplicate poll community"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
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

	poller := NewCommunityPoller(client, db, 10, nil, nil)
	sender := &duplicatePollTestSender{}
	dispatcher := newDuplicatePollTestDispatcher(db, newDuplicatePollTestCache(channelID, domain.AlarmTypeCommunity), sender)
	ctx := context.Background()

	require.NoError(t, poller.Poll(ctx, channelID))
	dispatcher.ProcessOnceForTest(ctx)
	require.Equal(t, 1, sender.count())

	requireDuplicatePollSingleSentState(t, db, domain.OutboxKindCommunityPost, postID)

	rewindDuplicatePollWatermark(t, db, channelID, domain.WatermarkTypeCommunityPost, lastContent)

	require.NoError(t, poller.Poll(ctx, channelID))
	dispatcher.ProcessOnceForTest(ctx)
	require.Equal(t, 1, sender.count())

	requireDuplicatePollSingleSentState(t, db, domain.OutboxKindCommunityPost, postID)

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	require.EqualValues(t, 1, postCount)
}

func TestShortsPollerDuplicatePollEnqueuesExactlyOnceAfterResolver(t *testing.T) {
	db := newBatchTestDB(t,
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

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-duplicate-poll"}}},"overlayMetadata":{"primaryText":{"content":"Short Duplicate Poll"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
</feed>`
	watchHTML := `
		<html>
			<head>
				<meta itemprop="uploadDate" content="2026-04-10T10:11:12+09:00">
			</head>
		</html>
	`

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
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

	poller := NewShortsPoller(client, db, 10, nil)
	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    client,
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := context.Background()

	require.NoError(t, poller.Poll(ctx, channelID))
	require.NoError(t, resolver.runOnce(ctx, time.Now().Add(time.Minute)))
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindNewShort, postID)

	rewindDuplicatePollWatermark(t, db, channelID, domain.WatermarkTypeShort, lastContent)

	require.NoError(t, poller.Poll(ctx, channelID))
	require.NoError(t, resolver.runOnce(ctx, time.Now().Add(2*time.Minute)))
	requireDuplicatePollSingleEnqueuedState(t, db, domain.OutboxKindNewShort, postID)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.EqualValues(t, 1, videoCount)
}

func newDuplicatePollTestDispatcher(db *gorm.DB, cacheSvc *cachemocks.Client, sender *duplicatePollTestSender) *outbox.Dispatcher {
	return outbox.NewDispatcher(
		db,
		cacheSvc,
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		outbox.Config{
			BatchSize:           10,
			LockTimeout:         time.Minute,
			PollInterval:        time.Second,
			MaxRetries:          3,
			RetryBackoff:        time.Minute,
			DeliveryParallelism: 1,
		},
	)
}

func newDuplicatePollTestCache(channelID string, alarmType domain.AlarmType) *cachemocks.Client {
	client := cachemocks.NewStrictClient()
	subscriberKey := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)

	client.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == subscriberKey {
			return []string{"room-once"}, nil
		}
		return nil, nil
	}
	client.HGetFunc = func(_ context.Context, key, field string) (string, error) {
		if key == "alarm:member_names" && field == channelID {
			return "Duplicate Poll Member", nil
		}
		return "", nil
	}

	return client
}

func rewindDuplicatePollWatermark(t *testing.T, db *gorm.DB, channelID string, watermarkType domain.WatermarkType, lastContentID string) {
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

func requireDuplicatePollSingleSentState(t *testing.T, db *gorm.DB, kind domain.OutboxKind, canonicalPostID string) {
	t.Helper()

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Where("kind = ?", kind).Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, canonicalPostID, outboxRows[0].ContentID)
	require.Equal(t, domain.OutboxStatusSent, outboxRows[0].Status)
	require.NotNil(t, outboxRows[0].SentAt)

	var deliveryRows []domain.YouTubeNotificationDelivery
	require.NoError(t, db.Where("outbox_id = ?", outboxRows[0].ID).Order("id ASC").Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 1)
	require.Equal(t, domain.OutboxStatusSent, deliveryRows[0].Status)
	require.NotNil(t, deliveryRows[0].SentAt)

	var trackingRow domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Where("kind = ? AND content_id = ?", kind, canonicalPostID).First(&trackingRow).Error)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)

	var stateRow domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.Where("kind = ? AND post_id = ?", kind, canonicalPostID).First(&stateRow).Error)
	require.Equal(t, canonicalPostID, stateRow.PostID)
	require.Equal(t, canonicalPostID, stateRow.ContentID)
	require.NotNil(t, stateRow.AlarmSentAt)
	require.Nil(t, stateRow.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, stateRow.DeliveryStatus)
}

func requireDuplicatePollSingleEnqueuedState(t *testing.T, db *gorm.DB, kind domain.OutboxKind, canonicalPostID string) {
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
	require.NotNil(t, stateRow.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, stateRow.DeliveryStatus)
}
