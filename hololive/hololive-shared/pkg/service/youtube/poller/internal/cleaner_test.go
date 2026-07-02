// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package polling

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestViewerSampleCleanerCleanupDeletesInBatches(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, ctx, pool, "old-video", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	insertViewerSampleCleanerLiveSession(t, ctx, pool, "recent-video", domain.LiveStatusEnded, now.AddDate(0, 0, -1))
	insertViewerSampleCleanerLiveSession(t, ctx, pool, "live-video", domain.LiveStatusLive, now.AddDate(0, 0, -8))
	for i := range 5 {
		insertViewerSampleCleanerSample(t, ctx, pool, "old-video", now.Add(time.Duration(i)*time.Second))
	}
	for i := range 2 {
		insertViewerSampleCleanerSample(t, ctx, pool, "recent-video", now.Add(time.Duration(i)*time.Minute))
	}
	insertViewerSampleCleanerSample(t, ctx, pool, "live-video", now)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 2})
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 5, deleted)
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, ctx, pool, "old-video"))
	require.EqualValues(t, 2, countViewerSampleCleanerSamples(t, ctx, pool, "recent-video"))
	require.EqualValues(t, 1, countViewerSampleCleanerSamples(t, ctx, pool, "live-video"))
}

func TestViewerSampleCleanerCleanupSkipsWhenLockNotAcquired(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, ctx, pool, "locked-video", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	for i := range 3 {
		insertViewerSampleCleanerSample(t, ctx, pool, "locked-video", now.Add(time.Duration(i)*time.Second))
	}

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, tx.Rollback(context.Background()))
	})
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", viewerSampleCleanupLockKey)
	require.NoError(t, err)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 2})
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, deleted)
	require.EqualValues(t, 3, countViewerSampleCleanerSamples(t, ctx, pool, "locked-video"))
}

func TestViewerSampleCleanerDeleteBatchDeletesExactlyBatchSize(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, ctx, pool, "old-video", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	for i := range 5 {
		insertViewerSampleCleanerSample(t, ctx, pool, "old-video", now.Add(time.Duration(i)*time.Second))
	}

	cutoff := now.AddDate(0, 0, -7)
	deleted, err := deleteViewerSampleCleanupBatch(ctx, pool, cutoff, 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 3, countViewerSampleCleanerSamples(t, ctx, pool, "old-video"))
}

func insertViewerSampleCleanerLiveSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	videoID string,
	status domain.LiveStatus,
	endedAt time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, ended_at)
		VALUES ($1, $2, $3, $4)`,
		videoID,
		"channel-"+videoID,
		status,
		endedAt,
	)
	require.NoError(t, err)
}

func insertViewerSampleCleanerSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, videoID string, capturedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_viewer_samples (video_id, captured_at, channel_id, concurrent_viewers)
		VALUES ($1, $2, $3, $4)`,
		videoID,
		capturedAt,
		"channel-"+videoID,
		100,
	)
	require.NoError(t, err)
}

func countViewerSampleCleanerSamples(t *testing.T, ctx context.Context, pool *pgxpool.Pool, videoID string) int64 {
	t.Helper()
	var count int64
	err := pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM youtube_live_viewer_samples WHERE video_id = $1",
		videoID,
	).Scan(&count)
	require.NoError(t, err)
	return count
}
