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

func TestPendingPublishedAtResolver_ReEnqueuesResolvedShortDispatchGapWithoutResolving(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
		&domain.YouTubeNotificationDeliveryTelemetry{},
	)
	detectedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	publishedAt := detectedAt.Add(-18 * time.Second)
	seedResolvedShortDispatchGap(t, db, "channel-1", "short-gap", detectedAt, publishedAt, nil)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt.Add(time.Hour), &resolveCalls),
		routeDecider: func(polling.NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 0, resolveCalls)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, domain.OutboxKindNewShort, outboxRows[0].Kind)
	assert.Equal(t, "short:short-gap", outboxRows[0].ContentID)
	assert.Contains(t, outboxRows[0].Payload, `"published_at": "`+publishedAt.Format(time.RFC3339)+`"`)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-gap").Error)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_ReleasesStaleResolvedShortDispatchGapClaim(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
		&domain.YouTubeNotificationDeliveryTelemetry{},
	)
	detectedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	publishedAt := detectedAt.Add(-18 * time.Second)
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	seedResolvedShortDispatchGap(t, db, "channel-1", "short-stale-gap", detectedAt, publishedAt, &authorizedAt)

	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt.Add(time.Hour), nil),
		routeDecider: func(polling.NotificationRouteRequest) bool {
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.Equal(t, "short:short-stale-gap", outboxRows[0].ContentID)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-stale-gap").Error)
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Nil(t, alarmState.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)
}

func TestPendingPublishedAtResolver_DropsResolvedShortDispatchGapWhenPublishedAtOlderThanRecoverWindow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
		&domain.YouTubeNotificationDeliveryTelemetry{},
	)
	detectedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	publishedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	seedResolvedShortDispatchGap(t, db, "channel-1", "short-old-gap", detectedAt, publishedAt, nil)

	routeCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt.Add(time.Hour), nil),
		routeDecider: func(polling.NotificationRouteRequest) bool {
			routeCalls++
			return true
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), time.Now().UTC()))

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 0, outboxCount)
	assert.Equal(t, 0, routeCalls)
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
	require.NoError(t, db.ExecTest(`
		CREATE TRIGGER fail_outbox_insert
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider: func(polling.NotificationRouteRequest) bool {
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
	require.NoError(t, db.ExecTest(`
		CREATE TRIGGER fail_outbox_insert_for_retry_after
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider:      func(polling.NotificationRouteRequest) bool { return true },
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
	require.NoError(t, db.ExecTest(`
		CREATE TRIGGER fail_outbox_insert_keep_retry_after
		BEFORE INSERT ON youtube_notification_outbox
		BEGIN
			SELECT RAISE(ABORT, 'outbox blocked');
		END;
	`).Error)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverTestClient(t, publishedAt, nil),
		routeDecider:      func(polling.NotificationRouteRequest) bool { return true },
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
