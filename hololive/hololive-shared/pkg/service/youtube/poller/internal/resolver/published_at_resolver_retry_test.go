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
		routeDecider: func(polling.NotificationRouteRequest) bool {
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
