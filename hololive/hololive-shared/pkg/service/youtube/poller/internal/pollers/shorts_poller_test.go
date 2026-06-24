package pollers

import (
	"bytes"
	"context"
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

func TestShortsPollerPollEnqueuesUnconditionallyAndSkipsRSSLookup(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "old-short",
	}).Error)

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	rssCalls := 0

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
					rssCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><feed></feed>`)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10)

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	metricBefore := testutil.ToFloat64(communityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)))
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, rssCalls, "RSS published_at enrichment must not run after community/shorts fadeout")

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at").Where("video_id = ?", "short-1").Take(&stored).Error)
	assert.Nil(t, stored.PublishedAt)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"short:short-1"`)
	assert.NotContains(t, outbox.Payload, `"published_at":`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Nil(t, sourcePost.ActualPublishedAt)
	assert.False(t, sourcePost.DetectedAt.IsZero())

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeShort).Error)
	assert.Equal(t, "short:short-1", watermark.LastContentID)

	entry := findLogEntryByMessage(t, &logBuffer, shortDetectedLogMessage)
	assert.Equal(t, "UC_TEST", entry[logschema.FieldChannelID])
	assert.Equal(t, "short:short-1", entry[logschema.FieldPostID])
	assert.Equal(t, yttimestamp.Format(tracking.DetectedAt), entry[logschema.FieldDetectedAt])

	batchEntry := findLogEntryByMessage(t, &logBuffer, logschema.CommunityShortsDetectionBatchMessage)
	assert.Equal(t, "UC_TEST", batchEntry[logschema.FieldChannelID])
	assert.Equal(t, string(domain.AlarmTypeShorts), batchEntry[logschema.FieldAlarmType])
	assert.Equal(t, float64(1), batchEntry[logschema.FieldDetectedCount])
	assert.Equal(t, yttimestamp.Format(tracking.DetectedAt), batchEntry[logschema.FieldDetectedAt])
	metricAfter := testutil.ToFloat64(communityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)))
	assert.Equal(t, float64(1), metricAfter-metricBefore)
}

func TestShortsPollerPollPersistsScrapeProvidedPublishedAt(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "old-short",
	}).Error)

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <entry>
    <yt:videoId>short-1</yt:videoId>
    <title>Short One</title>
    <published>2026-04-10T01:11:12+00:00</published>
  </entry>
</feed>`

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
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"short:short-1"`)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeShort).Error)
	assert.Equal(t, "short:short-1", watermark.LastContentID)
}

func TestShortsPollerPollDeduplicatesCollectedShortsByCanonicalPostID(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_DUPLICATE_SHORTS",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "old-short",
	}).Error)

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}},{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One Duplicate"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/shorts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10)

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	require.NoError(t, poller.Poll(context.Background(), "UC_DUPLICATE_SHORTS"))

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	assert.EqualValues(t, 1, videoCount)

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
	assert.Equal(t, "UC_DUPLICATE_SHORTS", batchEntry[logschema.FieldChannelID])
	assert.Equal(t, string(domain.AlarmTypeShorts), batchEntry[logschema.FieldAlarmType])
	assert.Equal(t, float64(1), batchEntry[logschema.FieldDetectedCount])
}

func TestShortsPoller_MissingPublishedAtEnqueuesImmediately(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "old-short",
	}).Error)

	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"short-1"}}},"overlayMetadata":{"primaryText":{"content":"Short One"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/1.jpg","width":120,"height":200}]}}}}}]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"
	watchCalls := 0

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/shorts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsHTML)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/watch":
					watchCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<html></html>")), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, watchCalls, "inline published_at /watch resolve must not run after fadeout")

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at").Where("video_id = ?", "short-1").Take(&stored).Error)
	assert.Nil(t, stored.PublishedAt)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"short:short-1"`)
	assert.Contains(t, outbox.Payload, `"video_id":"short-1"`)
	assert.NotContains(t, outbox.Payload, `"published_at":`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Nil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Nil(t, alarmState.ActualPublishedAt)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", "UC_TEST", domain.WatermarkTypeShort).Error)
	assert.Equal(t, "short:short-1", watermark.LastContentID)
}
