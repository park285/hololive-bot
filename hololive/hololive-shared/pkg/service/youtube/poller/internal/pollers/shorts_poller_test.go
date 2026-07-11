package pollers

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestShortsPollerPollResolvesFreshnessViaWatchPageAndSkipsRSSLookup(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_TEST", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("short-1", "old-short")
	}, func(videoID string, _ int) *http.Response {
		require.Equal(t, "short-1", videoID)
		return watchResponseWithPublishedAt(freshPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	metricBefore := testutil.ToFloat64(communityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)))
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.Zero(t, routes.rssCalls, "shorts freshness must not depend on RSS enrichment")
	assert.Equal(t, 1, routes.watchCalls["short-1"], "one watch-page resolve per unseen candidate")

	var stored struct {
		PublishedAt *time.Time
	}
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Select("published_at").Where("video_id = ?", "short-1").Take(&stored).Error)
	require.NotNil(t, stored.PublishedAt)
	assert.WithinDuration(t, freshPublishedAt, *stored.PublishedAt, time.Second)

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	assert.Contains(t, outbox.Payload, `"canonical_post_id":"short:short-1"`)
	assert.Contains(t, outbox.Payload, `"published_at":`)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.False(t, tracking.DetectedAt.IsZero())
	assert.Nil(t, tracking.AlarmSentAt)
	assert.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, tracking.DeliveryStatus)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.False(t, sourcePost.DetectedAt.IsZero())

	assert.Equal(t, "short:short-1", loadShortsWatermark(t, db, "UC_TEST").LastContentID)

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

func TestShortsPollerPollDeduplicatesCollectedShortsByCanonicalPostID(t *testing.T) {
	db := shortsFreshnessTestDB(t)
	seedShortsWatermark(t, db, "UC_DUPLICATE_SHORTS", "old-short")
	freshPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)

	routes := newShortsFreshnessRoutes(func() string {
		return shortsFreshnessTabHTML("short-1", "short-1", "old-short")
	}, func(videoID string, _ int) *http.Response {
		require.Equal(t, "short-1", videoID)
		return watchResponseWithPublishedAt(freshPublishedAt)
	})
	poller := NewShortsPoller(newShortsFreshnessClient(routes), db, 10)

	var logBuffer bytes.Buffer
	previousDefaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previousDefaultLogger)

	require.NoError(t, poller.Poll(context.Background(), "UC_DUPLICATE_SHORTS"))

	assert.Equal(t, 1, routes.watchCalls["short-1"], "collected duplicates must be merged before freshness resolve")
	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeVideo{}))
	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeNotificationOutbox{}))
	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeContentAlarmTracking{}))
	assert.EqualValues(t, 1, countRows(t, db, &domain.YouTubeCommunityShortsSourcePost{}))

	batchEntry := findLogEntryByMessage(t, &logBuffer, logschema.CommunityShortsDetectionBatchMessage)
	assert.Equal(t, "UC_DUPLICATE_SHORTS", batchEntry[logschema.FieldChannelID])
	assert.Equal(t, string(domain.AlarmTypeShorts), batchEntry[logschema.FieldAlarmType])
	assert.Equal(t, float64(1), batchEntry[logschema.FieldDetectedCount])
}

func TestClassifyShortByFreshnessUsesScrapeProvidedPublishedAtWithoutRemoteResolve(t *testing.T) {
	poller := &ShortsPoller{deferrals: newShortsFreshnessDeferrals()}
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour)

	classified := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "scrape-dated", PublishedAt: &fresh},
		"scrape-dated", shortVideoStateRow{}, false, now)

	assert.Equal(t, shortCandidateNotifyFresh, classified.class)
	require.NotNil(t, classified.publishedAt)
	assert.WithinDuration(t, fresh, *classified.publishedAt, time.Second)
}

func TestClassifyShortByFreshnessUsesKnownRowEvidenceWithoutRemoteResolve(t *testing.T) {
	poller := &ShortsPoller{deferrals: newShortsFreshnessDeferrals()}
	now := time.Now().UTC()
	oldPublishedAt := now.Add(-30 * 24 * time.Hour)
	oldFirstSeenAt := now.Add(-10 * 24 * time.Hour)

	rowDated := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "row-dated"},
		"row-dated",
		shortVideoStateRow{VideoID: "row-dated", PublishedAt: &oldPublishedAt, FirstSeenAt: oldFirstSeenAt},
		true, now)
	assert.Equal(t, shortCandidateStoreSilently, rowDated.class)

	rowUndated := poller.classifyShortByFreshness(context.Background(), "UC_UNIT",
		&scraper.Short{VideoID: "row-undated"},
		"row-undated",
		shortVideoStateRow{VideoID: "row-undated", FirstSeenAt: oldFirstSeenAt},
		true, now)
	assert.Equal(t, shortCandidateStoreSilently, rowUndated.class)
	assert.Nil(t, rowUndated.publishedAt)
}
