package polling

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestViewerSampleCleanerClampsWorkBudgets(t *testing.T) {
	cleaner := NewViewerSampleCleaner(nil, ViewerSampleCleanerConfig{
		BatchSize: viewerSampleCleanupMaxBatchSize * 10,
	})

	cleaner.maxBatches = viewerSampleCleanupMaxBatches * 10
	cleaner.maxDuration = viewerSampleCleanupMaxDuration * 10

	require.Equal(t, viewerSampleCleanupMaxBatchSize, cleaner.effectiveBatchSize())
	require.Equal(t, viewerSampleCleanupMaxBatches, cleaner.effectiveMaxBatches())
	require.Equal(t, viewerSampleCleanupMaxDuration, cleaner.effectiveMaxDuration())
}

func TestViewerSampleCleanerCleanupContinuesAfterShortSessionBatch(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, pool, "old-video-1", domain.LiveStatusEnded, now.AddDate(0, 0, -9))
	insertViewerSampleCleanerLiveSession(t, pool, "old-video-2", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	insertViewerSampleCleanerSample(t, pool, "old-video-1", now)
	insertViewerSampleCleanerSample(t, pool, "old-video-2", now)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 10})
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, pool, "old-video-1"))
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, pool, "old-video-2"))
}

func TestViewerSampleCleanerCleanupStopsAtBatchBudgetAndResumes(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	videoIDs := []string{"old-video-1", "old-video-2", "old-video-3"}

	for i, videoID := range videoIDs {
		insertViewerSampleCleanerLiveSession(t, pool, videoID, domain.LiveStatusEnded, now.AddDate(0, 0, -10+i))
		insertViewerSampleCleanerSample(t, pool, videoID, now)
	}

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 10})

	cleaner.maxBatches = 2
	cleaner.maxDuration = time.Minute

	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 1, countViewerSampleCleanerAllSamples(t, pool))

	deleted, err = cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.EqualValues(t, 0, countViewerSampleCleanerAllSamples(t, pool))
}

func TestViewerSampleCleanerCleanupResumesPastEmptyPagesAndMissingBoundary(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	oldEndedAt := now.AddDate(0, 0, -30)

	insertViewerSampleCleanerSessions(
		t,
		pool,
		"empty-",
		viewerSampleCleanupSessionPageSize*2+2,
		domain.LiveStatusEnded,
		oldEndedAt,
	)
	insertViewerSampleCleanerLiveSession(t, pool, "z-eligible", domain.LiveStatusEnded, oldEndedAt)
	insertViewerSampleCleanerSample(t, pool, "z-eligible", now)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 10})

	cleaner.maxBatches = 1
	cleaner.maxDuration = time.Minute

	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.Zero(t, deleted)

	firstBoundary := cleaner.state.cursor.videoID
	require.NotEmpty(t, firstBoundary)

	// 같은 tick의 live-session retention이 cursor 경계 행을 먼저 지워도 다음 호출은
	// tuple `>` keyset에서 이어져 유효한 다음 세션을 건너뛰지 않아야 한다.
	_, err = pool.Exec(ctx, "DELETE FROM youtube_live_sessions WHERE video_id = $1", firstBoundary)
	require.NoError(t, err)

	deleted, err = cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.Zero(t, deleted)

	deleted, err = cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, pool, "z-eligible"))
}

func TestViewerSampleCleanerCleanupStopsAtTimeBudget(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, pool, "old-video", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	insertViewerSampleCleanerSample(t, pool, "old-video", now)

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		rollbackViewerSampleCleanerTx(t, holder)
	})

	_, err = holder.Exec(ctx, "LOCK TABLE youtube_live_sessions IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 1})

	cleaner.maxBatches = 2
	cleaner.maxDuration = 50 * time.Millisecond

	deleted, err := cleaner.Cleanup(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, deleted)
	require.NoError(t, holder.Rollback(ctx))
	require.EqualValues(t, 1, countViewerSampleCleanerSamples(t, pool, "old-video"))
}

func TestViewerSampleCleanerCleanupRejectsNonSessionAffineQuerier(t *testing.T) {
	pool := dbtest.NewPool(t)
	cleaner := NewViewerSampleCleaner(viewerSampleQuerierOnly{Querier: pool}, ViewerSampleCleanerConfig{
		RetentionDays: 7,
		BatchSize:     2,
	})

	deleted, err := cleaner.Cleanup(t.Context())
	require.ErrorContains(t, err, "session-affine")
	require.Zero(t, deleted)
}

type viewerSampleQuerierOnly struct {
	dbx.Querier
}

func countViewerSampleCleanerAllSamples(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	ctx := t.Context()

	var count int64

	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM youtube_live_viewer_samples").Scan(&count)
	require.NoError(t, err)

	return count
}
