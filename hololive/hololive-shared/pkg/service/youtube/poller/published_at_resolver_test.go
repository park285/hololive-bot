package poller

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
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

func TestPendingPublishedAtResolver_EnqueuesOnceAfterResolution(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
		&domain.YouTubeNotificationDeliveryTelemetry{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-1", detectedAt)

	resolveCalls := 0
	client := newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls)
	var captured NotificationRouteRequest
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: client,
		routeDecider: func(req NotificationRouteRequest) bool {
			captured = req
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(2*time.Minute)))

	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeShorts,
		ChannelID:   "channel-1",
		PublishedAt: publishedAt,
	}, captured)

	assertResolvedShortState(t, db, "channel-1", "short-1", detectedAt, publishedAt)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxKindNewShort, outboxRows[0].Kind)
	assert.Equal(t, "short:short-1", outboxRows[0].ContentID)
	assert.Contains(t, outboxRows[0].Payload, `"canonical_post_id":"short:short-1"`)
	assert.Contains(t, outboxRows[0].Payload, `"published_at":"2026-04-10T01:11:12Z"`)
}

func TestPendingPublishedAtResolver_DoesNotDuplicateWhenAlarmStateAlreadyClaimed(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := detectedAt.Add(10 * time.Second)
	seedPendingShortResolution(t, db, "channel-1", "short-claim", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-claim").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newShortPublishedAtResolverTestClient(t, detectedAt.Add(-time.Minute), &resolveCalls),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Zero(t, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", "short-claim").Error)
	assert.Nil(t, video.PublishedAt)
}

func TestPendingPublishedAtResolver_UpdatesTrackingSourceStateAtomically(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-atomic", detectedAt)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_outbox_insert
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", "short-atomic").Error)
	assert.Nil(t, video.PublishedAt)

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-atomic").Error)
	assert.Nil(t, tracking.ActualPublishedAt)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-atomic").Error)
	assert.Nil(t, sourcePost.ActualPublishedAt)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-atomic").Error)
	assert.Nil(t, alarmState.ActualPublishedAt)
	assert.Nil(t, alarmState.AuthorizedAt)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
}

func TestPendingPublishedAtResolver_FinalizeFailureSetsRetryAfter(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-finalize-backoff", detectedAt)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_outbox_insert_for_retry_after
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider:      func(NotificationRouteRequest) bool { return true },
		interval:          15 * time.Second,
		batchSize:         50,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	beforeRun := time.Now().UTC()
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-finalize-backoff").Error)
	require.NotNil(t, alarmState.PublishedAtRetryAfter)
	assert.True(t, alarmState.PublishedAtRetryAfter.After(beforeRun))
}

func TestPendingPublishedAtResolver_FinalizeFailureDoesNotClearRetryAfter(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	retryAfter := time.Now().UTC().Add(5 * time.Minute)
	seedPendingShortResolution(t, db, "channel-1", "short-finalize-keep-retry", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-finalize-keep-retry").
		Update("published_at_retry_after", retryAfter).Error)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_outbox_insert_keep_retry_after
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider:      func(NotificationRouteRequest) bool { return true },
		interval:          15 * time.Second,
		batchSize:         50,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-finalize-keep-retry").Error)
	require.NotNil(t, alarmState.PublishedAtRetryAfter)
	assert.True(t, alarmState.PublishedAtRetryAfter.After(time.Now().UTC()))
}

func TestPendingPublishedAtResolver_SkipsAlreadySentContent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	sentAt := detectedAt.Add(3 * time.Minute)
	seedPendingShortResolution(t, db, "channel-1", "short-sent", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-sent").
		Updates(map[string]any{
			"alarm_sent_at":   sentAt,
			"delivery_status": domain.YouTubeContentAlarmDeliveryStatusSent,
		}).Error)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-sent").
		Updates(map[string]any{
			"alarm_sent_at":   sentAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newShortPublishedAtResolverTestClient(t, detectedAt.Add(-time.Minute), &resolveCalls),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Zero(t, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", "short-sent").Error)
	assert.Nil(t, video.PublishedAt)
}

func TestPendingPublishedAtResolver_RecordsMetricsAndEnqueueLog(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-metric", "short-metric", detectedAt)

	var buf bytes.Buffer
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewJSONHandler(&buf, nil)),
	}

	attemptBefore := testutil.ToFloat64(publishedAtResolutionAttemptTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	successBefore := testutil.ToFloat64(publishedAtResolutionSuccessTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	enqueuedBefore := testutil.ToFloat64(publishedAtResolverEnqueuedTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	scannedBefore := testutil.ToFloat64(publishedAtResolverScannedTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	attemptAfter := testutil.ToFloat64(publishedAtResolutionAttemptTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	successAfter := testutil.ToFloat64(publishedAtResolutionSuccessTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	enqueuedAfter := testutil.ToFloat64(publishedAtResolverEnqueuedTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	scannedAfter := testutil.ToFloat64(publishedAtResolverScannedTotal.WithLabelValues(string(domain.OutboxKindNewShort)))

	assert.Equal(t, float64(1), attemptAfter-attemptBefore)
	assert.Equal(t, float64(1), successAfter-successBefore)
	assert.Equal(t, float64(1), enqueuedAfter-enqueuedBefore)
	assert.Equal(t, float64(1), scannedAfter-scannedBefore)
	assert.Equal(t, float64(1), testutil.ToFloat64(publishedAtResolverPageCandidates))
	assert.Contains(t, buf.String(), `"msg":"published_at_resolver_enqueued"`)
	assert.Contains(t, buf.String(), `"kind":"NEW_SHORT"`)
	assert.Contains(t, buf.String(), `"post_id":"short:short-metric"`)
	assert.Contains(t, buf.String(), `"channel_id":"channel-metric"`)
	assert.Contains(t, buf.String(), `"reason":"resolved"`)
}

func TestPendingPublishedAtResolver_RecordsFailureMetric(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-fail", "short-fail", detectedAt)

	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newShortPublishedAtResolverErrorClient(t),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	failureBefore := testutil.ToFloat64(publishedAtResolutionFailureTotal.WithLabelValues(string(domain.OutboxKindNewShort)))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	failureAfter := testutil.ToFloat64(publishedAtResolutionFailureTotal.WithLabelValues(string(domain.OutboxKindNewShort)))

	assert.Equal(t, float64(1), failureAfter-failureBefore)
}

func TestPendingPublishedAtResolver_RespectsMaxResolvePerRun(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-1", detectedAt)
	seedPendingShortResolution(t, db, "channel-1", "short-2", detectedAt.Add(time.Second))
	seedPendingShortResolution(t, db, "channel-1", "short-3", detectedAt.Add(2*time.Second))

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:               db,
		client:           newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider:     func(NotificationRouteRequest) bool { return true },
		interval:         15 * time.Second,
		batchSize:        3,
		maxResolvePerRun: 2,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 2, resolveCalls)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("content_id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 2)
	assert.Equal(t, "short:short-1", outboxRows[0].ContentID)
	assert.Equal(t, "short:short-2", outboxRows[1].ContentID)

	var unresolvedVideo domain.YouTubeVideo
	require.NoError(t, db.First(&unresolvedVideo, "video_id = ?", "short-3").Error)
	assert.Nil(t, unresolvedVideo.PublishedAt)
}

func TestPendingPublishedAtResolver_StopsWhenMaxRunDurationExceeded(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-1", detectedAt)
	seedPendingShortResolution(t, db, "channel-1", "short-2", detectedAt.Add(time.Second))
	seedPendingShortResolution(t, db, "channel-1", "short-3", detectedAt.Add(2*time.Second))

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:               db,
		client:           newShortPublishedAtResolverDelayedClient(t, publishedAt, 15*time.Millisecond, &resolveCalls),
		routeDecider:     func(NotificationRouteRequest) bool { return true },
		interval:         15 * time.Second,
		batchSize:        3,
		maxResolvePerRun: 3,
		maxRunDuration:   time.Nanosecond,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	assert.Equal(t, 0, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Equal(t, int64(0), outboxCount)
}

func TestPendingPublishedAtResolver_SkipsFreshCandidatesBeforeMinDetectedAge(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	now := time.Now().UTC()
	oldDetectedAt := now.Add(-time.Minute)
	freshDetectedAt := now.Add(-5 * time.Second)
	publishedAt := now.Add(-2 * time.Minute)
	seedPendingShortResolution(t, db, "channel-aged", "short-aged", oldDetectedAt)
	seedPendingShortResolution(t, db, "channel-fresh", "short-fresh", freshDetectedAt)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:               db,
		client:           newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider:     func(NotificationRouteRequest) bool { return true },
		interval:         time.Hour,
		batchSize:        10,
		maxResolvePerRun: 10,
		minDetectedAge:   20 * time.Second,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		resolver.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 1, resolveCalls)

	var agedVideo domain.YouTubeVideo
	require.NoError(t, db.First(&agedVideo, "video_id = ?", "short-aged").Error)
	require.NotNil(t, agedVideo.PublishedAt)

	var freshVideo domain.YouTubeVideo
	require.NoError(t, db.First(&freshVideo, "video_id = ?", "short-fresh").Error)
	assert.Nil(t, freshVideo.PublishedAt)
}

func TestPendingPublishedAtResolver_SetsRetryAfterOnFailure(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-backoff", "short-backoff", detectedAt)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverHTTPClient(t, `<html><body>missing upload date</body></html>`, &resolveCalls),
		interval:          15 * time.Second,
		batchSize:         10,
		maxResolvePerRun:  10,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(2*time.Minute)))

	assert.Equal(t, 1, resolveCalls)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-backoff").Error)
	require.NotNil(t, alarmState.PublishedAtRetryAfter)
}

func TestPendingPublishedAtResolver_SetsRetryAfterOnPublishedAtEmpty(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-empty", "short-empty", detectedAt)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverHTTPClient(t, `<html><body>no date</body></html>`),
		interval:          15 * time.Second,
		batchSize:         10,
		maxResolvePerRun:  10,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "published_at_empty"))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "published_at_empty"))

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-empty").Error)
	require.NotNil(t, alarmState.PublishedAtRetryAfter)
	assert.Equal(t, float64(1), skippedAfter-skippedBefore)
}

func TestPendingPublishedAtResolver_ClearsRetryAfterOnSuccess(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-clear", "short-clear", detectedAt)
	retryAfter := detectedAt.Add(5 * time.Minute)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-clear").
		Update("published_at_retry_after", retryAfter).Error)

	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-clear").Error)
	assert.Nil(t, alarmState.PublishedAtRetryAfter)
}

func TestPendingPublishedAtResolver_FailedCandidateDoesNotStarveFollowingCandidate(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	firstDetectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	secondDetectedAt := firstDetectedAt.Add(time.Second)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	seedPendingShortResolution(t, db, "channel-1", "short-bad", firstDetectedAt)
	seedPendingShortResolution(t, db, "channel-1", "short-good", secondDetectedAt)
	require.NoError(t, db.Delete(&domain.YouTubeVideo{}, "video_id = ?", "short-bad").Error)

	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		interval:  15 * time.Second,
		batchSize: 1,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), secondDetectedAt.Add(time.Minute)))

	var badOutboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).
		Where("kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-bad").
		Count(&badOutboxCount).Error)
	assert.Zero(t, badOutboxCount)

	var goodOutbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&goodOutbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-good").Error)
	assert.Equal(t, domain.OutboxStatusPending, goodOutbox.Status)

	var goodVideo domain.YouTubeVideo
	require.NoError(t, db.First(&goodVideo, "video_id = ?", "short-good").Error)
	require.NotNil(t, goodVideo.PublishedAt)
	assert.Equal(t, publishedAt.UTC(), goodVideo.PublishedAt.UTC())
}

func seedPendingShortResolution(t *testing.T, db *gorm.DB, channelID, videoID string, detectedAt time.Time) {
	t.Helper()

	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     videoID,
		ChannelID:   channelID,
		Title:       "Short " + videoID,
		IsShort:     true,
		ViewCount:   10,
		FirstSeenAt: detectedAt,
		LastSeenAt:  detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindNewShort,
		ContentID:          "short:" + videoID,
		CanonicalContentID: "short:" + videoID,
		ChannelID:          channelID,
		DetectedAt:         detectedAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusPending,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsSourcePost{
		Kind:       domain.OutboxKindNewShort,
		PostID:     "short:" + videoID,
		ChannelID:  channelID,
		DetectedAt: detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:" + videoID,
		ContentID:      "short:" + videoID,
		ChannelID:      channelID,
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}).Error)
}

func newShortPublishedAtResolverTestClient(t *testing.T, publishedAt time.Time, resolveCalls *int) *scraper.Client {
	t.Helper()

	watchHTML := `<html><head><meta itemprop="uploadDate" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return newShortPublishedAtResolverHTTPClient(t, watchHTML, resolveCalls)
}

func newShortPublishedAtResolverHTTPClient(t *testing.T, watchHTML string, resolveCalls ...*int) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					if len(resolveCalls) > 0 && resolveCalls[0] != nil {
						*resolveCalls[0] = *resolveCalls[0] + 1
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func newShortPublishedAtResolverDelayedClient(t *testing.T, publishedAt time.Time, delay time.Duration, resolveCalls *int) *scraper.Client {
	t.Helper()

	watchHTML := `<html><head><meta itemprop="uploadDate" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					time.Sleep(delay)
					if resolveCalls != nil {
						*resolveCalls = *resolveCalls + 1
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func newShortPublishedAtResolverErrorClient(t *testing.T) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					return nil, assert.AnError
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func assertResolvedShortState(t *testing.T, db *gorm.DB, channelID, videoID string, detectedAt, publishedAt time.Time) {
	t.Helper()

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", videoID).Error)
	require.NotNil(t, video.PublishedAt)
	assert.Equal(t, publishedAt, video.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt, tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt, tracking.DetectedAt.UTC())

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt, alarmState.ActualPublishedAt.UTC())
	require.NotNil(t, alarmState.AuthorizedAt)
	assert.Equal(t, channelID, alarmState.ChannelID)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
}
