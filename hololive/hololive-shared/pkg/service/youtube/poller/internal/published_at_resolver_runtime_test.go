package polling

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

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

	var resolveCalls atomic.Int32
	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            newShortPublishedAtResolverDelayedHTTPClientWithCallback(t, `<html><body>no date</body></html>`, 40*time.Millisecond, func() { resolveCalls.Add(1) }),
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

	require.Eventually(t, func() bool {
		return resolveCalls.Load() >= 2
	}, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, int32(2), resolveCalls.Load())
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
		client:           newShortPublishedAtResolverDelayedClient(t, publishedAt, 180*time.Millisecond, &resolveCalls),
		routeDecider:     func(NotificationRouteRequest) bool { return true },
		interval:         15 * time.Second,
		batchSize:        2,
		maxResolvePerRun: 2,
		maxRunDuration:   480 * time.Millisecond,
		resolveTimeout:   400 * time.Millisecond,
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

func TestPendingPublishedAtResolver_AdmissionDeferredDoesNotSetCandidateRetryAfter(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	detectedAt := time.Date(2026, 4, 10, 1, 11, 30, 0, time.UTC)
	seedPendingShortResolution(t, db, "channel-admission", "short-admission", detectedAt)

	limiter := scraper.NewRateLimiter(time.Hour)
	decision, err := limiter.TryReserve(context.Background())
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	resolver := &PendingPublishedAtResolver{
		db:                db,
		client:            scraper.NewClient(scraper.WithRateLimiter(limiter)),
		interval:          15 * time.Second,
		batchSize:         1,
		maxResolvePerRun:  1,
		maxRunDuration:    200 * time.Millisecond,
		resolveTimeout:    100 * time.Millisecond,
		failureBackoffTTL: time.Minute,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err = resolver.runOnce(context.Background(), detectedAt.Add(time.Minute))
	require.Error(t, err)
	require.True(t, scraper.IsAdmissionDeferred(err), "err = %v", err)

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-admission").Error)
	require.Nil(t, alarmState.PublishedAtRetryAfter)
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
	// 고정 sleep 후 cancel하면 부하 시 RunOnce persist 도중 ctx가 취소돼 flaky하다.
	// aged 후보의 published_at이 실제 커밋된 것을 확인한 뒤 cancel한다.
	require.Eventually(t, func() bool {
		var v domain.YouTubeVideo
		if err := db.First(&v, "video_id = ?", "short-aged").Error; err != nil {
			return false
		}
		return v.PublishedAt != nil
	}, 2*time.Second, 10*time.Millisecond)
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

	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	if !errors.Is(err, context.Canceled) {
		assert.Zero(t, resolveCalls, "resolver must stop before resolving when ctx is canceled")
	}
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
	require.NoError(t, db.ExecTest(`
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
