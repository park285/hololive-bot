package resolver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		routeDecider:     func(polling.NotificationRouteRequest) bool { return true },
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
		routeDecider:     func(polling.NotificationRouteRequest) bool { return true },
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
