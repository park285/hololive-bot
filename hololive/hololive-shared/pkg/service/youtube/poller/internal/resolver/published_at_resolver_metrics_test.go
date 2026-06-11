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
