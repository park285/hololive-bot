package resolver

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
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
	seedPendingCommunityResolution(t, db, "post-claim", detectedAt)
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
	seedPendingCommunityResolution(t, db, "post-stale-claim", detectedAt)
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
	seedPendingCommunityResolution(t, db, "post-stale-outbox", detectedAt)
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
	seedPendingCommunityResolution(t, db, "post-sent", detectedAt)
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
	seedPendingCommunityResolution(t, db, "post-clear", detectedAt)
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
