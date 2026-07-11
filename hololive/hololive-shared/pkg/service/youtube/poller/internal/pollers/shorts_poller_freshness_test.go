package pollers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func shortsFreshnessTabHTML(videoIDs ...string) string {
	items := make([]string, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		items = append(items, fmt.Sprintf(
			`{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":%q}}},"overlayMetadata":{"primaryText":{"content":"Short %s"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/%s.jpg","width":120,"height":200}]}}}}}`,
			videoID, videoID, videoID,
		))
	}
	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[` +
		strings.Join(items, ",") + `]}}}}]}}}`
	return "<script>var ytInitialData = " + shortsJSON + ";</script>"
}

func shortsWatchPageHTML(publishedAt time.Time) string {
	return fmt.Sprintf(`<html><head><meta itemprop="datePublished" content=%q></head><body></body></html>`,
		publishedAt.UTC().Format(time.RFC3339))
}

type shortsFreshnessRoutes struct {
	shortsHTML func() string
	watch      func(videoID string, calls int) *http.Response
	watchCalls map[string]int
	rssCalls   int
	totalWatch int
}

func newShortsFreshnessRoutes(shortsHTML func() string, watch func(videoID string, calls int) *http.Response) *shortsFreshnessRoutes {
	return &shortsFreshnessRoutes{
		shortsHTML: shortsHTML,
		watch:      watch,
		watchCalls: make(map[string]int),
	}
}

func (r *shortsFreshnessRoutes) roundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case strings.HasSuffix(req.URL.Path, "/shorts"):
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(r.shortsHTML())), Header: make(http.Header), Request: req}, nil
	case req.URL.Path == "/watch":
		videoID := req.URL.Query().Get("v")
		r.watchCalls[videoID]++
		r.totalWatch++
		if r.watch == nil {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
		}
		return r.watch(videoID, r.watchCalls[videoID]), nil
	case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
		r.rssCalls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><feed></feed>`)), Header: make(http.Header), Request: req}, nil
	default:
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
	}
}

func watchResponseWithPublishedAt(publishedAt time.Time) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsWatchPageHTML(publishedAt))), Header: make(http.Header)}
}

func watchResponseWithoutPublishedAt() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<html><head></head><body></body></html>")), Header: make(http.Header)}
}

func watchResponseServerError() *http.Response {
	return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("boom")), Header: make(http.Header)}
}

func newShortsFreshnessClient(routes *shortsFreshnessRoutes) *scraper.Client {
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout:   5 * time.Second,
			Transport: shortsPollerRoundTripFunc(routes.roundTrip),
		}),
	)
}

func shortsFreshnessTestDB(t *testing.T) *pollerBatchTestDB {
	t.Helper()
	return newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
}

func seedShortsWatermark(t *testing.T, db *pollerBatchTestDB, channelID, lastContentID string) {
	t.Helper()
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: lastContentID,
	}).Error)
}

func loadShortsWatermark(t *testing.T, db *pollerBatchTestDB, channelID string) domain.YouTubeContentWatermark {
	t.Helper()
	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.First(&watermark, "channel_id = ? AND watermark_type = ?", channelID, domain.WatermarkTypeShort).Error)
	return watermark
}

func countRows(t *testing.T, db *pollerBatchTestDB, model any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	return count
}

func TestShortsPollerInitialBaselinePollStoresAllWithoutNotificationsOrFetches(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("base-1", "base-2", "base-3")
	}, nil)
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_BASELINE"))

	assert.EqualValues(t, 3, countRows(t, db, &domain.YouTubeVideo{}))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeContentAlarmTracking{}))
	assert.Zero(t, routes.totalWatch, "baseline poll must not resolve published_at remotely")

	watermark := loadShortsWatermark(t, db, "UC_BASELINE")
	assert.True(t, watermark.Initialized)
	assert.Equal(t, "short:base-1", watermark.LastContentID)
}

func TestShortsPollerNotifiesFreshUnseenShortExactlyOnceAcrossPolls(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_FRESH", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("fresh-new", "old-short")
	}, func(videoID string, _ int) *http.Response {
		require.Equal(t, "fresh-new", videoID)
		return watchResponseWithPublishedAt(freshPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_FRESH"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:fresh-new", outboxRows[0].ContentID)
	assert.Equal(t, domain.OutboxStatusPending, outboxRows[0].Status)
	assert.Contains(t, outboxRows[0].Payload, `"published_at":`)

	var stored struct {
		PublishedAt *time.Time
		IsShort     bool
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at, is_short").Where("video_id = ?", "fresh-new").Take(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.WithinDuration(t, freshPublishedAt, *stored.PublishedAt, time.Second)
	assert.True(t, stored.IsShort)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:fresh-new").Error)
	require.NotNil(t, tracking.ActualPublishedAt)

	assert.Equal(t, "short:fresh-new", loadShortsWatermark(t, db, "UC_FRESH").LastContentID)
	assert.Equal(t, 1, routes.totalWatch)

	require.NoError(t, poller.Poll(context.Background(), "UC_FRESH"))
	require.NoError(t, poller.Poll(context.Background(), "UC_FRESH"))

	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.Equal(t, 1, routes.totalWatch, "known short must not be re-resolved")
}

func TestShortsPollerWatermarkMissingNotifiesOnlyVerifiedFreshShorts(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_ANOMALY", "short:gone-from-page")
	seenAt := time.Now().UTC().Add(-120 * 24 * time.Hour)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "known-hist",
		ChannelID:   "UC_ANOMALY",
		Title:       "Known Historical",
		IsShort:     true,
		FirstSeenAt: seenAt,
		LastSeenAt:  seenAt,
	}).Error)

	freshPublishedAt := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	oldPublishedAt := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("known-hist", "unseen-hist", "genuine-new")
	}, func(videoID string, _ int) *http.Response {
		switch videoID {
		case "unseen-hist":
			return watchResponseWithPublishedAt(oldPublishedAt)
		case "genuine-new":
			return watchResponseWithPublishedAt(freshPublishedAt)
		default:
			t.Errorf("unexpected published_at resolve for %s", videoID)
			return watchResponseServerError()
		}
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_ANOMALY"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1, "only the verified-fresh short may be notified")
	assert.Equal(t, "short:genuine-new", outboxRows[0].ContentID)

	var trackingRows []domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Order("content_id ASC").Find(&trackingRows).Error)
	require.Len(t, trackingRows, 1)
	assert.Equal(t, "short:genuine-new", trackingRows[0].ContentID)

	var unseenHist struct {
		PublishedAt *time.Time
		IsShort     bool
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at, is_short").Where("video_id = ?", "unseen-hist").Take(&unseenHist).Error)
	require.NotNil(t, unseenHist.PublishedAt, "historical short must be stored with its resolved published_at")
	assert.WithinDuration(t, oldPublishedAt, *unseenHist.PublishedAt, time.Second)
	assert.True(t, unseenHist.IsShort)

	assert.Equal(t, 0, routes.watchCalls["known-hist"], "known short must not be re-resolved")
	assert.Equal(t, "short:known-hist", loadShortsWatermark(t, db, "UC_ANOMALY").LastContentID)
}

func TestShortsPollerPartialReorderKeepsUnseenHistoricalSilent(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_REORDER", "old-short")
	oldPublishedAt := time.Now().UTC().Add(-60 * 24 * time.Hour).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("unseen-hist", "old-short")
	}, func(videoID string, _ int) *http.Response {
		require.Equal(t, "unseen-hist", videoID)
		return watchResponseWithPublishedAt(oldPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_REORDER"))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeContentAlarmTracking{}))

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at").Where("video_id = ?", "unseen-hist").Take(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.Equal(t, "short:unseen-hist", loadShortsWatermark(t, db, "UC_REORDER").LastContentID)
}

func TestShortsPollerDefersUnverifiableCandidateAndNotifiesWhenResolved(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_DEFER", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("flaky-new", "old-short")
	}, func(videoID string, calls int) *http.Response {
		require.Equal(t, "flaky-new", videoID)
		if calls == 1 {
			return watchResponseWithoutPublishedAt()
		}
		return watchResponseWithPublishedAt(freshPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_DEFER"))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var deferredCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Where("video_id = ?", "flaky-new").Count(&deferredCount).Error)
	assert.EqualValues(t, 0, deferredCount, "deferred candidate must not be absorbed into youtube_videos")
	assert.Equal(t, "old-short", loadShortsWatermark(t, db, "UC_DEFER").LastContentID,
		"watermark must hold while a candidate is deferred")

	require.NoError(t, poller.Poll(context.Background(), "UC_DEFER"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:flaky-new", outboxRows[0].ContentID)
	assert.Equal(t, "short:flaky-new", loadShortsWatermark(t, db, "UC_DEFER").LastContentID)

	require.NoError(t, poller.Poll(context.Background(), "UC_DEFER"))
	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.Equal(t, 2, routes.totalWatch)
}

func TestShortsPollerAbsorbsCandidateSilentlyAfterFreshnessAttemptsExhausted(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_ABSORB", "old-short")

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("dateless", "old-short")
	}, func(videoID string, _ int) *http.Response {
		require.Equal(t, "dateless", videoID)
		return watchResponseWithoutPublishedAt()
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_ABSORB"))
	require.NoError(t, poller.Poll(context.Background(), "UC_ABSORB"))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeVideo{}))
	assert.Equal(t, "old-short", loadShortsWatermark(t, db, "UC_ABSORB").LastContentID)

	require.NoError(t, poller.Poll(context.Background(), "UC_ABSORB"))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	var absorbed struct {
		PublishedAt *time.Time
		IsShort     bool
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at, is_short").Where("video_id = ?", "dateless").Take(&absorbed).Error)
	assert.Nil(t, absorbed.PublishedAt)
	assert.True(t, absorbed.IsShort)
	assert.Equal(t, "short:dateless", loadShortsWatermark(t, db, "UC_ABSORB").LastContentID)
	assert.Equal(t, 3, routes.totalWatch)

	require.NoError(t, poller.Poll(context.Background(), "UC_ABSORB"))
	assert.Equal(t, 3, routes.totalWatch, "absorbed short must not be re-resolved")
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
}

func TestShortsPollerKnownNonShortRowSuppressesReexposureWithoutRefetch(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_NONSHORT", "old-short")
	oldPublishedAt := time.Now().UTC().Add(-150 * 24 * time.Hour).Truncate(time.Second)
	oldSeenAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "video-row-dated",
		ChannelID:   "UC_NONSHORT",
		Title:       "Stored As Video",
		IsShort:     false,
		PublishedAt: &oldPublishedAt,
		FirstSeenAt: oldSeenAt,
		LastSeenAt:  oldSeenAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "video-row-undated",
		ChannelID:   "UC_NONSHORT",
		Title:       "Stored As Video Without PublishedAt",
		IsShort:     false,
		FirstSeenAt: oldSeenAt,
		LastSeenAt:  oldSeenAt,
	}).Error)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("video-row-dated", "video-row-undated", "old-short")
	}, nil)
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_NONSHORT"))

	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeContentAlarmTracking{}))
	assert.Zero(t, routes.totalWatch, "known rows with local age evidence must not trigger remote resolves")

	var stored struct {
		IsShort bool
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("is_short").Where("video_id = ?", "video-row-dated").Take(&stored).Error)
	assert.False(t, stored.IsShort, "is_short classification must stay untouched for NEW_VIDEO dedup")
	assert.Equal(t, "short:video-row-dated", loadShortsWatermark(t, db, "UC_NONSHORT").LastContentID)
}

func TestShortsPollerReoffersKnownShortAboveWatermarkToRearmFailedOutbox(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_REARM", "old-short")
	seenAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	publishedAt := seenAt.Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "failed-short",
		ChannelID:   "UC_REARM",
		Title:       "Failed Short",
		IsShort:     true,
		PublishedAt: &publishedAt,
		FirstSeenAt: seenAt,
		LastSeenAt:  seenAt,
	}).Error)
	createdAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	failedOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_REARM",
		ContentID:     "short:failed-short",
		Payload:       polling.BuildShortNotificationPayload(&domain.YouTubeVideo{VideoID: "failed-short", PublishedAt: &publishedAt}, "short:failed-short"),
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		Error:         "delivery exhausted",
	}
	require.NoError(t, db.Create(&failedOutbox).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID:      failedOutbox.ID,
		RoomID:        "room-1",
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		Error:         "delivery failed",
	}).Error)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("failed-short", "old-short")
	}, nil)
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_REARM"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxStatusPending, outboxRows[0].Status)
	assert.Equal(t, 0, outboxRows[0].AttemptCount)
	assert.Contains(t, outboxRows[0].Payload, `"published_at":"`+yttimestamp.Format(publishedAt)+`"`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:failed-short").Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.WithinDuration(t, publishedAt, *tracking.ActualPublishedAt, time.Second)

	var deliveryRows []domain.YouTubeNotificationDelivery
	require.NoError(t, db.Order("id ASC").Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 1)
	assert.Equal(t, domain.OutboxStatusPending, deliveryRows[0].Status)

	assert.Zero(t, routes.totalWatch, "known short must not trigger a published_at resolve")
	assert.Equal(t, "short:failed-short", loadShortsWatermark(t, db, "UC_REARM").LastContentID)
}

func TestShortsPollerKeepsSentOutboxUntouchedOnReexposure(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_SENT", "old-short")
	seenAt := time.Now().UTC().Add(-45 * 24 * time.Hour)
	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     "sent-short",
		ChannelID:   "UC_SENT",
		Title:       "Sent Short",
		IsShort:     true,
		FirstSeenAt: seenAt,
		LastSeenAt:  seenAt,
	}).Error)
	sentAt := time.Now().UTC().Add(-44 * 24 * time.Hour)
	sentOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_SENT",
		ContentID:     "short:sent-short",
		Payload:       `{"canonical_post_id":"short:sent-short","video_id":"sent-short"}`,
		Status:        domain.OutboxStatusSent,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&sentOutbox).Error)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("sent-short", "old-short")
	}, nil)
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_SENT"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxStatusSent, outboxRows[0].Status)
	require.NotNil(t, outboxRows[0].SentAt)
	assert.Zero(t, routes.totalWatch)
}

func TestShortsPollerHoldsWatermarkWhileDeferredCandidateIsMissingFromPage(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_DEPART", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)

	pageIDs := []string{"flaky-new", "old-short"}
	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML(pageIDs...)
	}, func(videoID string, _ int) *http.Response {
		if videoID == "other-new" {
			return watchResponseWithPublishedAt(freshPublishedAt)
		}
		return watchResponseWithoutPublishedAt()
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_DEPART"))
	assert.Equal(t, "old-short", loadShortsWatermark(t, db, "UC_DEPART").LastContentID)

	pageIDs = []string{"other-new", "old-short"}
	require.NoError(t, poller.Poll(context.Background(), "UC_DEPART"))
	assert.Equal(t, "old-short", loadShortsWatermark(t, db, "UC_DEPART").LastContentID,
		"watermark must stay held while a deferred candidate is missing from the page")

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:other-new", outboxRows[0].ContentID)

	require.NoError(t, poller.Poll(context.Background(), "UC_DEPART"))
	assert.Equal(t, "short:other-new", loadShortsWatermark(t, db, "UC_DEPART").LastContentID,
		"watermark hold must release after the departed candidate exhausts its attempts")

	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, 1, routes.watchCalls["flaky-new"])
	assert.Equal(t, 1, routes.watchCalls["other-new"])
}

func TestShortsPollerReevaluatesDeferredCandidateWhenItReturnsToPage(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_RETURN", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)

	pageIDs := []string{"flaky-new", "old-short"}
	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML(pageIDs...)
	}, func(videoID string, calls int) *http.Response {
		require.Equal(t, "flaky-new", videoID)
		if calls == 1 {
			return watchResponseWithoutPublishedAt()
		}
		return watchResponseWithPublishedAt(freshPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	require.NoError(t, poller.Poll(context.Background(), "UC_RETURN"))

	pageIDs = []string{"old-short"}
	require.NoError(t, poller.Poll(context.Background(), "UC_RETURN"))
	assert.EqualValues(t, 0, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.Equal(t, "old-short", loadShortsWatermark(t, db, "UC_RETURN").LastContentID)

	pageIDs = []string{"flaky-new", "old-short"}
	require.NoError(t, poller.Poll(context.Background(), "UC_RETURN"))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:flaky-new", outboxRows[0].ContentID)
	assert.Equal(t, "short:flaky-new", loadShortsWatermark(t, db, "UC_RETURN").LastContentID)
	assert.Equal(t, 2, routes.watchCalls["flaky-new"])
}

func TestClassifyShortByFreshnessTreatsFarFuturePublishedAtAsUnresolved(t *testing.T) {
	poller := &ShortsPoller{deferrals: newShortsFreshnessDeferrals()}
	now := time.Now().UTC()
	farFuture := now.Add(48 * time.Hour)
	nearFuture := now.Add(30 * time.Minute)

	first := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "future-short", PublishedAt: &farFuture},
		"future-short", shortVideoStateRow{}, false, now)
	assert.Equal(t, shortCandidateDeferred, first.class, "far-future published_at must defer, not notify")

	second := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "future-short", PublishedAt: &farFuture},
		"future-short", shortVideoStateRow{}, false, now)
	assert.Equal(t, shortCandidateDeferred, second.class)

	third := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "future-short", PublishedAt: &farFuture},
		"future-short", shortVideoStateRow{}, false, now)
	assert.Equal(t, shortCandidateStoreSilently, third.class, "far-future candidate must absorb silently after max attempts")
	assert.Nil(t, third.publishedAt, "absorbed far-future candidate must not persist the suspicious published_at")

	nearFresh := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "near-future-short", PublishedAt: &nearFuture},
		"near-future-short", shortVideoStateRow{}, false, now)
	assert.Equal(t, shortCandidateNotifyFresh, nearFresh.class, "clock-skew-level future published_at stays fresh")
}
