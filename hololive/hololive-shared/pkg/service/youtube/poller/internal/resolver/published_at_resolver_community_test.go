package resolver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingPublishedAtResolver_BackfillsMetadataForAlreadySentContentWithoutDuplicateEnqueue(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	sentAt := detectedAt.Add(3 * time.Minute)
	publishedAt := detectedAt.Add(-time.Minute)
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
		client:    newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	assertShortMetadataBackfilledWithoutEnqueue(t, db, "short-sent", detectedAt, publishedAt, nil, &sentAt)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-sent").Error)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_ClearsRetryAfterAfterMetadataOnlyBackfill(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-10 * time.Second)
	publishedAt := detectedAt.Add(-time.Minute)
	retryAfter := detectedAt.Add(5 * time.Minute)
	seedPendingShortResolution(t, db, "channel-clear", "short-clear-metadata", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-clear-metadata").
		Updates(map[string]any{
			"authorized_at":            authorizedAt,
			"published_at_retry_after": retryAfter,
			"delivery_status":          domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assertShortMetadataBackfilledWithoutEnqueue(t, db, "short-clear-metadata", detectedAt, publishedAt, &authorizedAt, nil)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-clear-metadata").Error)
	assert.Nil(t, alarmState.PublishedAtRetryAfter)
}

func TestPendingPublishedAtResolver_BackfillsCommunityMetadataWithoutDuplicateWhenAlarmStateAlreadyClaimed(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-10 * time.Second)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingCommunityResolution(t, db, "channel-community", "post-claim", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-claim").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newCommunityPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	assertCommunityMetadataBackfilledWithoutEnqueue(t, db, "post-claim", detectedAt, publishedAt, &authorizedAt, nil)
}

func TestPendingPublishedAtResolver_ReEnqueuesWhenStaleCommunityClaimHasNoOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingCommunityResolution(t, db, "channel-community", "post-stale-claim", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-claim").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newCommunityPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider: func(polling.NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "community:post-stale-claim").Error)
	require.NotNil(t, post.PublishedAt)
	assert.Equal(t, publishedAt.UTC(), post.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-claim").Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt.UTC(), tracking.DetectedAt.UTC())
	assert.Nil(t, tracking.AlarmSentAt)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-claim").Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), sourcePost.ActualPublishedAt.UTC())

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxKindCommunityPost, outboxRows[0].Kind)
	assert.Equal(t, "community:post-stale-claim", outboxRows[0].ContentID)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-claim").Error)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_DoesNotDuplicateWhenStaleCommunityClaimHasOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingCommunityResolution(t, db, "channel-community", "post-stale-outbox", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-outbox").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "channel-community",
		ContentID:     "community:post-stale-outbox",
		Payload:       `{"post_id":"community:post-stale-outbox","canonical_post_id":"community:post-stale-outbox"}`,
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: time.Now().UTC(),
	}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newCommunityPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider: func(polling.NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)
	assertCommunityMetadataBackfilledWithoutEnqueue(t, db, "post-stale-outbox", detectedAt, publishedAt, &authorizedAt, nil)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "community:post-stale-outbox", outboxRows[0].ContentID)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-stale-outbox").Error)
	require.NotNil(t, alarmState.AuthorizedAt)
	assert.Equal(t, authorizedAt.UTC(), alarmState.AuthorizedAt.UTC())
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_BackfillsCommunityMetadataForAlreadySentContentWithoutDuplicateEnqueue(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	sentAt := detectedAt.Add(3 * time.Minute)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingCommunityResolution(t, db, "channel-community", "post-sent", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:post-sent").
		Updates(map[string]any{
			"alarm_sent_at":   sentAt,
			"delivery_status": domain.YouTubeContentAlarmDeliveryStatusSent,
		}).Error)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-sent").
		Updates(map[string]any{
			"alarm_sent_at":   sentAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newCommunityPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	assertCommunityMetadataBackfilledWithoutEnqueue(t, db, "post-sent", detectedAt, publishedAt, nil, &sentAt)
}

func TestPendingPublishedAtResolver_ClearsRetryAfterAfterCommunityMetadataOnlyBackfill(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-10 * time.Second)
	publishedAt := detectedAt.Add(-time.Minute)
	retryAfter := detectedAt.Add(5 * time.Minute)
	seedPendingCommunityResolution(t, db, "channel-community", "post-clear", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-clear").
		Updates(map[string]any{
			"authorized_at":            authorizedAt,
			"published_at_retry_after": retryAfter,
			"delivery_status":          domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolver := &PendingPublishedAtResolver{
		db:        db,
		client:    newCommunityPublishedAtResolverTestClient(t, publishedAt, nil),
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assertCommunityMetadataBackfilledWithoutEnqueue(t, db, "post-clear", detectedAt, publishedAt, &authorizedAt, nil)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-clear").Error)
	assert.Nil(t, alarmState.PublishedAtRetryAfter)
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
		routeDecider: func(polling.NotificationRouteRequest) bool {
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
		routeDecider:     func(polling.NotificationRouteRequest) bool { return true },
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
		routeDecider:     func(polling.NotificationRouteRequest) bool { return true },
		interval:         15 * time.Second,
		batchSize:        3,
		maxResolvePerRun: 3,
		maxRunDuration:   9 * time.Second,
		resolveTimeout:   10 * time.Second,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "run_budget_exhausted"))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "run_budget_exhausted"))
	assert.Equal(t, 0, resolveCalls)
	assert.Equal(t, float64(1), skippedAfter-skippedBefore)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Equal(t, int64(0), outboxCount)
}
