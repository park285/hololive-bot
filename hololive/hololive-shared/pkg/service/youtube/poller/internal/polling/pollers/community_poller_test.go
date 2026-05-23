package pollers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestCommunityPollerPollPersistsPublishedAtAndDetectedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
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

	captured := NotificationRouteRequest{}
	poller := NewCommunityPoller(client, db, 10, nil, func(req NotificationRouteRequest) bool {
		captured = req
		return true
	})

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	metricBefore := testutil.ToFloat64(communityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	canonicalPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	assert.Equal(t, NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeCommunity,
		ChannelID:   "UC_TEST",
		PublishedAt: canonicalPublishedAt,
	}, captured)

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("published_at").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), stored.PublishedAt.Format(time.RFC3339Nano))

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"community:post-1"`)
	assert.Contains(t, outbox.Payload, `"post_id":"post-1"`)
	assert.Contains(t, outbox.Payload, `"published_at":"`+yttimestamp.Format(canonicalPublishedAt)+`"`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), tracking.ActualPublishedAt.Format(time.RFC3339Nano))
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), sourcePost.ActualPublishedAt.Format(time.RFC3339Nano))
	assert.False(t, sourcePost.DetectedAt.IsZero())

	entry := findLogEntryByMessage(t, &logBuffer, communityPostDetectedLogMessage)
	assert.Equal(t, "UC_TEST", entry[logschema.FieldChannelID])
	assert.Equal(t, "community:post-1", entry[logschema.FieldPostID])
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), entry[logschema.FieldActualPublishedAt])
	assert.Equal(t, yttimestamp.Format(tracking.DetectedAt), entry[logschema.FieldDetectedAt])

	batchEntry := findLogEntryByMessage(t, &logBuffer, logschema.CommunityShortsDetectionBatchMessage)
	assert.Equal(t, "UC_TEST", batchEntry[logschema.FieldChannelID])
	assert.Equal(t, string(domain.AlarmTypeCommunity), batchEntry[logschema.FieldAlarmType])
	assert.Equal(t, float64(1), batchEntry[logschema.FieldDetectedCount])
	assert.Equal(t, yttimestamp.Format(tracking.DetectedAt), batchEntry[logschema.FieldDetectedAt])
	metricAfter := testutil.ToFloat64(communityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))
	assert.Equal(t, float64(1), metricAfter-metricBefore)
}

func TestCommunityPollerPollTreatsCanonicalWatermarkAsSameUpstreamPostID(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "community:post-1",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
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
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	assert.Zero(t, postCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var trackingCount int64
	require.NoError(t, db.Model(&domain.YouTubeContentAlarmTracking{}).Count(&trackingCount).Error)
	assert.Zero(t, trackingCount)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).First(&watermark).Error)
	assert.Equal(t, "community:post-1", watermark.LastContentID)
}

func TestCommunityPollerPollKeepsCanonicalIDStableAcrossRescrapes(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	firstPostsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	secondPostsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"navigationEndpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/post/post-1?lc=1"}}},"authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"},"navigationEndpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/post/post-1?lc=1"}}}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + firstPostsJSON + ";</script>"

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
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	postsHTML = "<script>var ytInitialData = " + secondPostsJSON + ";</script>"
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	require.EqualValues(t, 1, postCount)

	var stored struct {
		PostID string
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("post_id").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	assert.Equal(t, "community:post-1", stored.PostID)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.EqualValues(t, 1, outboxCount)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"community:post-1"`)
	assert.Contains(t, outbox.Payload, `"post_id":"post-1"`)

	var trackingCount int64
	require.NoError(t, db.Model(&domain.YouTubeContentAlarmTracking{}).Count(&trackingCount).Error)
	require.EqualValues(t, 1, trackingCount)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).First(&watermark).Error)
	assert.Equal(t, "community:post-1", watermark.LastContentID)
}

func TestCommunityPollerPollDeduplicatesCollectedPostsByCanonicalPostID(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_DUPLICATE_COMMUNITY",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_DUPLICATE_COMMUNITY"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}},{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"navigationEndpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/post/post-1?lc=1"}}},"authorEndpoint":{"browseEndpoint":{"browseId":"UC_DUPLICATE_COMMUNITY"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"publishedTimeText":{"simpleText":"2026-04-10T10:11:12+09:00"},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"},"navigationEndpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/post/post-1?lc=1"}}}}}}}}}}}]}}}}}]}}}`
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

	routeCalls := 0
	poller := NewCommunityPoller(client, db, 10, nil, func(req NotificationRouteRequest) bool {
		routeCalls++
		return true
	})

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	require.NoError(t, poller.Poll(context.Background(), "UC_DUPLICATE_COMMUNITY"))

	assert.Equal(t, 1, routeCalls)

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	assert.EqualValues(t, 1, postCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)

	var trackingCount int64
	require.NoError(t, db.Model(&domain.YouTubeContentAlarmTracking{}).Count(&trackingCount).Error)
	assert.EqualValues(t, 1, trackingCount)

	var sourcePostCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsSourcePost{}).Count(&sourcePostCount).Error)
	assert.EqualValues(t, 1, sourcePostCount)

	batchEntry := findLogEntryByMessage(t, &logBuffer, logschema.CommunityShortsDetectionBatchMessage)
	assert.Equal(t, "UC_DUPLICATE_COMMUNITY", batchEntry[logschema.FieldChannelID])
	assert.Equal(t, string(domain.AlarmTypeCommunity), batchEntry[logschema.FieldAlarmType])
	assert.Equal(t, float64(1), batchEntry[logschema.FieldDetectedCount])
}

func findLogEntryByMessage(t *testing.T, logBuffer *bytes.Buffer, message string) map[string]any {
	t.Helper()

	for line := range bytes.SplitSeq(bytes.TrimSpace(logBuffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		entry := make(map[string]any)
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal log entry: %v", err)
		}
		if entry["msg"] == message {
			return entry
		}
	}

	t.Fatalf("log message %q not found in %s", message, logBuffer.String())
	return nil
}

func TestCommunityPoller_MissingPublishedAtWithNilRouteDeciderEnqueuesImmediately(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"
	postHTML := `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head></html>`

	resolveCalls := 0
	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/post/post-1":
					resolveCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewCommunityPoller(client, db, 10, nil, nil)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, resolveCalls)

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("published_at").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	assert.Nil(t, stored.PublishedAt)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"community:post-1"`)
	assert.Contains(t, outbox.Payload, `"post_id":"post-1"`)
	assert.NotContains(t, outbox.Payload, `"published_at":`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, tracking.DetectedAt, sourcePost.DetectedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, alarmState.ActualPublishedAt)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).Error)
	assert.Equal(t, "community:post-1", watermark.LastContentID)
}

func TestCommunityPoller_PublishedAtMissingStillAdvancesWatermark(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"

	resolveCalls := 0
	routeCalls := 0
	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/post/post-1":
					resolveCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<html></html>")), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewCommunityPoller(client, db, 10, nil, func(NotificationRouteRequest) bool {
		routeCalls++
		return true
	})
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, resolveCalls)
	assert.Zero(t, routeCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, tracking.DetectedAt, sourcePost.DetectedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, alarmState.ActualPublishedAt)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).Error)
	assert.Equal(t, "community:post-1", watermark.LastContentID)
}

func TestCommunityPoller_InlineFallbackResolvesPublishedAtAndEnqueues(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"
	postHTML := `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head></html>`
	resolveCalls := 0

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/post/post-1":
					resolveCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	captured := NotificationRouteRequest{}
	poller := NewCommunityPoller(client, db, 10, nil, func(req NotificationRouteRequest) bool {
		captured = req
		return true
	}, true)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	canonicalPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeCommunity,
		ChannelID:   "UC_TEST",
		PublishedAt: canonicalPublishedAt,
	}, captured)

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("published_at").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), stored.PublishedAt.Format(time.RFC3339Nano))

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Contains(t, outbox.Payload, `"published_at":"`+yttimestamp.Format(canonicalPublishedAt)+`"`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, yttimestamp.Format(canonicalPublishedAt), tracking.ActualPublishedAt.Format(time.RFC3339Nano))
}

func TestCommunityPoller_InlineFallbackNotFoundSkipsNotification(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"
	resolveCalls := 0
	routeCalls := 0

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/post/post-1":
					resolveCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<html></html>")), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewCommunityPoller(client, db, 10, nil, func(NotificationRouteRequest) bool {
		routeCalls++
		return true
	}, true)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Equal(t, 1, resolveCalls)
	assert.Zero(t, routeCalls)

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("published_at").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	assert.Nil(t, stored.PublishedAt)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, tracking.DetectedAt, sourcePost.DetectedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, alarmState.ActualPublishedAt)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).Error)
	assert.Equal(t, "old-post", watermark.LastContentID)
}

func TestCommunityPoller_InlineFallbackResolveErrorWarnsAndSkipsNotification(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)

	postsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Posts","content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backstagePostThreadRenderer":{"post":{"backstagePostRenderer":{"postId":"post-1","authorEndpoint":{"browseEndpoint":{"browseId":"UC_TEST"}},"authorText":{"runs":[{"text":"Author"}]},"authorThumbnail":{"thumbnails":[{"url":"https://img.test/a.jpg","width":88,"height":88}]},"contentText":{"runs":[{"text":"hello world"}]},"voteCount":{"simpleText":"1.2K"},"actionButtons":{"commentActionButtonsRenderer":{"replyButton":{"buttonRenderer":{"text":{"simpleText":"7"}}}}}}}}}]}}}}}]}}}`
	postsHTML := "<script>var ytInitialData = " + postsJSON + ";</script>"
	routeCalls := 0

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/posts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/post/post-1":
					return nil, errors.New("resolver boom")
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	poller := NewCommunityPoller(client, db, 10, nil, func(NotificationRouteRequest) bool {
		routeCalls++
		return true
	}, true)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, routeCalls)
	assert.Contains(t, logBuffer.String(), "community published_at inline fallback failed")

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Select("published_at").Where("post_id = ?", "community:post-1").Take(&stored).Error)
	assert.Nil(t, stored.PublishedAt)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeCommunityPost).Error)
	assert.Equal(t, "old-post", watermark.LastContentID)
}
