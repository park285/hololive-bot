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

func TestPendingPublishedAtResolver_BackfillsMetadataWithoutDuplicateWhenAlarmStateAlreadyClaimed(t *testing.T) {
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

	assertShortMetadataBackfilledWithoutEnqueue(t, db, "short-claim", detectedAt, publishedAt, &authorizedAt, nil)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-claim").Error)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_ReEnqueuesWhenStaleShortClaimHasNoOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingShortResolution(t, db, "channel-1", "short-stale-claim", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-stale-claim").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)
	assertResolvedShortState(t, db, "channel-1", "short-stale-claim", detectedAt, publishedAt)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxKindNewShort, outboxRows[0].Kind)
	assert.Equal(t, "short:short-stale-claim", outboxRows[0].ContentID)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-stale-claim").Error)
	require.NotNil(t, alarmState.AuthorizedAt)
	assert.NotEqual(t, authorizedAt.UTC(), alarmState.AuthorizedAt.UTC())
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_DoesNotDuplicateWhenStaleShortClaimHasOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingShortResolution(t, db, "channel-1", "short-stale-outbox", detectedAt)
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-stale-outbox").
		Updates(map[string]any{
			"authorized_at":   authorizedAt,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		}).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "channel-1",
		ContentID:     "short:short-stale-outbox",
		Payload:       `{"canonical_post_id":"short:short-stale-outbox"}`,
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: time.Now().UTC(),
	}).Error)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)
	assertShortMetadataBackfilledWithoutEnqueue(t, db, "short-stale-outbox", detectedAt, publishedAt, &authorizedAt, nil)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:short-stale-outbox", outboxRows[0].ContentID)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-stale-outbox").Error)
	require.NotNil(t, alarmState.AuthorizedAt)
	assert.Equal(t, authorizedAt.UTC(), alarmState.AuthorizedAt.UTC())
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
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
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute)
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
		routeDecider: func(NotificationRouteRequest) bool {
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
	require.NotNil(t, alarmState.AuthorizedAt)
	assert.NotEqual(t, authorizedAt.UTC(), alarmState.AuthorizedAt.UTC())
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, alarmState.DeliveryStatus)
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
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute)
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
		routeDecider: func(NotificationRouteRequest) bool {
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

func TestPendingPublishedAtResolver_StartWaitsIntervalAfterCompletion(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	now := time.Now().UTC()
	seedPendingShortResolution(t, db, "channel-1", "short-1", now.Add(-2*time.Minute))

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverDelayedHTTPClient(t, `<html><body>no date</body></html>`, 40*time.Millisecond, &resolveCalls),
		interval:          50 * time.Millisecond,
		batchSize:         1,
		maxResolvePerRun:  1,
		maxRunDuration:    500 * time.Millisecond,
		resolveTimeout:    200 * time.Millisecond,
		minDetectedAge:    20 * time.Second,
		failureBackoffTTL: time.Millisecond,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		resolver.Start(ctx)
	}()

	time.Sleep(145 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 2, resolveCalls)
}

func TestPendingPublishedAtResolver_SkipsCandidateWhenRemainingRunBudgetIsTooSmall(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Now().UTC().Add(-time.Minute)
	publishedAt := detectedAt.Add(-time.Minute)
	seedPendingShortResolution(t, db, "channel-1", "short-1", detectedAt)
	seedPendingShortResolution(t, db, "channel-1", "short-2", detectedAt.Add(time.Second))

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:               db,
		client:           newShortPublishedAtResolverDelayedClient(t, publishedAt, 80*time.Millisecond, &resolveCalls),
		routeDecider:     func(NotificationRouteRequest) bool { return true },
		interval:         15 * time.Second,
		batchSize:        2,
		maxResolvePerRun: 2,
		maxRunDuration:   150 * time.Millisecond,
		resolveTimeout:   100 * time.Millisecond,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "run_budget_exhausted"))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "run_budget_exhausted"))

	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, float64(1), skippedAfter-skippedBefore)

	var firstVideo domain.YouTubeVideo
	require.NoError(t, db.First(&firstVideo, "video_id = ?", "short-1").Error)
	require.NotNil(t, firstVideo.PublishedAt)

	var secondVideo domain.YouTubeVideo
	require.NoError(t, db.First(&secondVideo, "video_id = ?", "short-2").Error)
	assert.Nil(t, secondVideo.PublishedAt)
}

func TestPendingPublishedAtResolver_UsesPerCandidateResolveTimeout(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-timeout", "short-timeout", detectedAt)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverDelayedClient(t, detectedAt.Add(-time.Minute), 80*time.Millisecond, &resolveCalls),
		interval:          15 * time.Second,
		batchSize:         1,
		maxResolvePerRun:  1,
		maxRunDuration:    200 * time.Millisecond,
		resolveTimeout:    20 * time.Millisecond,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "resolve_timeout"))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "resolve_timeout"))

	assert.Zero(t, resolveCalls)
	assert.Equal(t, float64(1), skippedAfter-skippedBefore)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-timeout").Error)
	require.NotNil(t, alarmState.PublishedAtRetryAfter)
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

func TestPendingPublishedAtResolver_CancelDuringCandidateResolutionReturnsParentCancel(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-cancel", "short-cancel", detectedAt)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverDelayedClient(t, detectedAt.Add(-time.Minute), 80*time.Millisecond, &resolveCalls),
		interval:          15 * time.Second,
		batchSize:         1,
		maxResolvePerRun:  1,
		maxRunDuration:    200 * time.Millisecond,
		resolveTimeout:    100 * time.Millisecond,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "resolve_timeout"))
	err := resolver.runOnce(ctx, detectedAt.Add(time.Minute))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "resolve_timeout"))

	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, resolveCalls)
	assert.Equal(t, float64(0), skippedAfter-skippedBefore)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-cancel").Error)
	assert.Nil(t, alarmState.PublishedAtRetryAfter)
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

func TestPendingPublishedAtResolver_SurfacesRetryAfterWriteFailures(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-write-fail", "short-write-fail", detectedAt)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_retry_after_update
		BEFORE UPDATE OF published_at_retry_after ON youtube_community_shorts_alarm_states
		BEGIN
			SELECT RAISE(ABORT, 'retry_after blocked');
		END;
	`).Error)

	var buf bytes.Buffer
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverHTTPClient(t, `<html><body>no date</body></html>`),
		interval:          15 * time.Second,
		batchSize:         10,
		maxResolvePerRun:  10,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewJSONHandler(&buf, nil)),
	}

	skippedBefore := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "retry_after_write_failed"))
	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))
	skippedAfter := testutil.ToFloat64(publishedAtResolverSkippedTotal.WithLabelValues(string(domain.OutboxKindNewShort), "retry_after_write_failed"))

	assert.Equal(t, float64(1), skippedAfter-skippedBefore)
	assert.Contains(t, buf.String(), `"msg":"Pending published_at resolver failed to write retry_after"`)
	assert.Contains(t, buf.String(), `"post_id":"short:short-write-fail"`)
	assert.Contains(t, buf.String(), `"reason":"published_at_empty"`)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-write-fail").Error)
	assert.Nil(t, alarmState.PublishedAtRetryAfter)
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

func seedPendingCommunityResolution(t *testing.T, db *gorm.DB, channelID, postID string, detectedAt time.Time) {
	t.Helper()

	require.NoError(t, db.Create(&domain.YouTubeCommunityPost{
		PostID:       "community:" + postID,
		ChannelID:    channelID,
		AuthorName:   "Author " + postID,
		ContentText:  "Content " + postID,
		LikeCount:    10,
		CommentCount: 1,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          "community:" + postID,
		CanonicalContentID: "community:" + postID,
		ChannelID:          channelID,
		DetectedAt:         detectedAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusPending,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsSourcePost{
		Kind:       domain.OutboxKindCommunityPost,
		PostID:     "community:" + postID,
		ChannelID:  channelID,
		DetectedAt: detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindCommunityPost,
		PostID:         "community:" + postID,
		ContentID:      "community:" + postID,
		ChannelID:      channelID,
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}).Error)
}

func assertShortMetadataBackfilledWithoutEnqueue(
	t *testing.T,
	db *gorm.DB,
	videoID string,
	detectedAt time.Time,
	publishedAt time.Time,
	authorizedAt *time.Time,
	alarmSentAt *time.Time,
) {
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
	if alarmSentAt == nil {
		assert.Nil(t, tracking.AlarmSentAt)
	} else {
		require.NotNil(t, tracking.AlarmSentAt)
		assert.Equal(t, *alarmSentAt, tracking.AlarmSentAt.UTC())
	}

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt, alarmState.ActualPublishedAt.UTC())
	if authorizedAt == nil {
		assert.Nil(t, alarmState.AuthorizedAt)
	} else {
		require.NotNil(t, alarmState.AuthorizedAt)
		assert.Equal(t, *authorizedAt, alarmState.AuthorizedAt.UTC())
	}
	if alarmSentAt == nil {
		assert.Nil(t, alarmState.AlarmSentAt)
	} else {
		require.NotNil(t, alarmState.AlarmSentAt)
		assert.Equal(t, *alarmSentAt, alarmState.AlarmSentAt.UTC())
	}
}

func assertCommunityMetadataBackfilledWithoutEnqueue(
	t *testing.T,
	db *gorm.DB,
	postID string,
	detectedAt time.Time,
	publishedAt time.Time,
	authorizedAt *time.Time,
	alarmSentAt *time.Time,
) {
	t.Helper()

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "community:"+postID).Error)
	require.NotNil(t, post.PublishedAt)
	assert.Equal(t, publishedAt.UTC(), post.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt.UTC(), tracking.DetectedAt.UTC())
	if alarmSentAt == nil {
		assert.Nil(t, tracking.AlarmSentAt)
	} else {
		require.NotNil(t, tracking.AlarmSentAt)
		assert.Equal(t, alarmSentAt.UTC(), tracking.AlarmSentAt.UTC())
	}

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), alarmState.ActualPublishedAt.UTC())
	if authorizedAt == nil {
		assert.Nil(t, alarmState.AuthorizedAt)
	} else {
		require.NotNil(t, alarmState.AuthorizedAt)
		assert.Equal(t, authorizedAt.UTC(), alarmState.AuthorizedAt.UTC())
	}
	if alarmSentAt == nil {
		assert.Nil(t, alarmState.AlarmSentAt)
	} else {
		require.NotNil(t, alarmState.AlarmSentAt)
		assert.Equal(t, alarmSentAt.UTC(), alarmState.AlarmSentAt.UTC())
	}
}

func newShortPublishedAtResolverTestClient(t *testing.T, publishedAt time.Time, resolveCalls *int) *scraper.Client {
	t.Helper()

	watchHTML := `<html><head><meta itemprop="uploadDate" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return newShortPublishedAtResolverHTTPClient(t, watchHTML, resolveCalls)
}

func newCommunityPublishedAtResolverTestClient(t *testing.T, publishedAt time.Time, resolveCalls *int) *scraper.Client {
	t.Helper()

	postHTML := `<html><head><meta itemprop="datePublished" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasPrefix(req.URL.Path, "/post/") {
					if resolveCalls != nil {
						*resolveCalls = *resolveCalls + 1
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
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
	return newShortPublishedAtResolverDelayedHTTPClient(t, watchHTML, delay, resolveCalls)
}

func newShortPublishedAtResolverDelayedHTTPClient(t *testing.T, watchHTML string, delay time.Duration, resolveCalls *int) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-timer.C:
					}
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
