package polling

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
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

func TestPendingPublishedAtResolver_SkipsCandidateWhenPeerOwned(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-peer", detectedAt)

	resolveCalls := 0
	claimer := &schedulerClaimStub{
		status: JobClaimStatus{Result: JobClaimPeerOwned, RetryAfter: time.Minute},
	}
	resolver := &PendingPublishedAtResolver{
		db:               db,
		client:           newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		interval:         15 * time.Second,
		batchSize:        50,
		candidateClaimer: claimer,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, claimer.tryCalls)
	assert.Equal(t, PendingPublishedAtResolverCandidatePollerName, claimer.poller)
	assert.Equal(t, "NEW_SHORT:short:short-peer", claimer.channelID)
	assert.Equal(t, 0, resolveCalls)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
}

func TestPendingPublishedAtResolver_CompletesCandidateClaimAfterFinalize(t *testing.T) {
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
	seedPendingShortResolution(t, db, "channel-1", "short-claim", detectedAt)

	resolveCalls := 0
	claim := &schedulerClaimHandleStub{}
	claimer := &schedulerClaimStub{
		status: JobClaimStatus{Result: JobClaimAcquired},
		claim:  claim,
	}
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		routeDecider: func(NotificationRouteRequest) bool {
			return true
		},
		interval:         15 * time.Second,
		batchSize:        50,
		candidateClaimer: claimer,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	require.NoError(t, resolver.runOnce(context.Background(), detectedAt.Add(time.Minute)))

	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, claim.markCompletedCalls)
	assert.Equal(t, 0, claim.releaseCalls)
}

func TestPendingPublishedAtResolver_FailsClosedWhenCandidateClaimUnavailable(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-1", "short-unavailable", detectedAt)

	resolveCalls := 0
	resolver := &PendingPublishedAtResolver{
		db:     db,
		client: newShortPublishedAtResolverTestClient(t, publishedAt, &resolveCalls),
		candidateClaimer: &schedulerClaimStub{
			status: JobClaimStatus{Result: JobClaimUnavailable},
			err:    assert.AnError,
		},
		interval:  15 * time.Second,
		batchSize: 50,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := resolver.runOnce(context.Background(), detectedAt.Add(time.Minute))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim pending published_at candidate")
	assert.Equal(t, 0, resolveCalls)
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
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
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
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)
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
	authorizedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
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
		routeDecider: func(NotificationRouteRequest) bool {
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
	assert.Contains(t, outboxRows[0].Payload, `"published_at":"`+publishedAt.Format(time.RFC3339)+`"`)

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
		routeDecider: func(NotificationRouteRequest) bool {
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
		routeDecider: func(NotificationRouteRequest) bool {
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
