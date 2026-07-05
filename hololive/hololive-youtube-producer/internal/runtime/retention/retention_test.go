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

package retention

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
)

func TestCleanupRetentionZeroDeletesNothing(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertStatsHistory(t, ctx, pool, "ch-old", now.AddDate(0, 0, -100))
	insertChannelSnapshot(t, ctx, pool, "ch-old", now.AddDate(0, 0, -100))
	insertLiveSession(t, ctx, pool, "vid-old", "ENDED", new(now.AddDate(0, 0, -100)))

	cleaner := NewCleaner(pool, Config{BatchSize: 10}, nil)
	require.False(t, cleaner.config.Enabled())

	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, deleted)
	require.EqualValues(t, 1, countRows(t, ctx, pool, "youtube_stats_history"))
	require.EqualValues(t, 1, countRows(t, ctx, pool, "youtube_channel_stats_snapshots"))
	require.EqualValues(t, 1, countRows(t, ctx, pool, "youtube_live_sessions"))
}

func TestCleanupDeletesOnlyBeforeCutoffInBatches(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	for i := range 5 {
		insertChannelSnapshot(t, ctx, pool, fmt.Sprintf("cs-old-%d", i), now.AddDate(0, 0, -40).Add(time.Duration(i)*time.Minute))
	}
	for i := range 2 {
		insertChannelSnapshot(t, ctx, pool, fmt.Sprintf("cs-recent-%d", i), now.AddDate(0, 0, -1))
	}

	cleaner := NewCleaner(pool, Config{ChannelSnapshotsDays: 30, BatchSize: 2}, nil)
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 5, deleted)
	require.EqualValues(t, 2, countRows(t, ctx, pool, "youtube_channel_stats_snapshots"))
}

func TestCleanupLiveSessionsDeletesOnlyEnded(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertLiveSession(t, ctx, pool, "ended-old", "ENDED", new(now.AddDate(0, 0, -40)))
	insertLiveSession(t, ctx, pool, "ended-recent", "ENDED", new(now.AddDate(0, 0, -1)))
	insertLiveSession(t, ctx, pool, "live-old", "LIVE", new(now.AddDate(0, 0, -40)))
	insertLiveSession(t, ctx, pool, "upcoming", "UPCOMING", nil)

	cleaner := NewCleaner(pool, Config{LiveSessionsDays: 30, BatchSize: 10}, nil)
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.False(t, existsLiveSession(t, ctx, pool, "ended-old"))
	require.True(t, existsLiveSession(t, ctx, pool, "ended-recent"))
	require.True(t, existsLiveSession(t, ctx, pool, "live-old"))
	require.True(t, existsLiveSession(t, ctx, pool, "upcoming"))
}

func TestCleanupLiveSessionsSkipsSessionsWithViewerSamples(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertLiveSession(t, ctx, pool, "ended-with-samples", "ENDED", new(now.AddDate(0, 0, -40)))
	insertLiveSession(t, ctx, pool, "ended-no-samples", "ENDED", new(now.AddDate(0, 0, -40)))
	insertViewerSample(t, ctx, pool, "ended-with-samples", now.AddDate(0, 0, -40))

	cleaner := NewCleaner(pool, Config{LiveSessionsDays: 30, BatchSize: 10}, nil)
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.True(t, existsLiveSession(t, ctx, pool, "ended-with-samples"))
	require.False(t, existsLiveSession(t, ctx, pool, "ended-no-samples"))
	require.EqualValues(t, 1, countRows(t, ctx, pool, "youtube_live_viewer_samples"))
}

func TestCleanupViewerSamplesUnlocksLiveSessionsInSameTick(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertLiveSession(t, ctx, pool, "ended-with-samples", "ENDED", new(now.AddDate(0, 0, -40)))
	insertViewerSample(t, ctx, pool, "ended-with-samples", now.AddDate(0, 0, -40))

	cleaner := NewCleaner(pool, Config{ViewerSamplesDays: 30, LiveSessionsDays: 30, BatchSize: 10}, nil)
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 0, countRows(t, ctx, pool, "youtube_live_viewer_samples"))
	require.False(t, existsLiveSession(t, ctx, pool, "ended-with-samples"))
}

func TestStartStopsOnContextCancel(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	cleaner := NewCleaner(pool, Config{ChannelSnapshotsDays: 30, BatchSize: 10, Interval: time.Hour}, nil)

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cleaner.Start(loopCtx)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func insertStatsHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channelID string, at time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO youtube_stats_history (time, channel_id, subscribers) VALUES ($1, $2, $3)`,
		at, channelID, 100,
	)
	require.NoError(t, err)
}

func insertChannelSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channelID string, capturedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO youtube_channel_stats_snapshots (channel_id, captured_at, subscriber_count) VALUES ($1, $2, $3)`,
		channelID, capturedAt, 100,
	)
	require.NoError(t, err)
}

func insertLiveSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, videoID, status string, endedAt *time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO youtube_live_sessions (video_id, channel_id, status, ended_at) VALUES ($1, $2, $3, $4)`,
		videoID, "channel-"+videoID, status, endedAt,
	)
	require.NoError(t, err)
}

func insertViewerSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, videoID string, capturedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO youtube_live_viewer_samples (video_id, captured_at, channel_id, concurrent_viewers) VALUES ($1, $2, $3, $4)`,
		videoID, capturedAt, "channel-"+videoID, 100,
	)
	require.NoError(t, err)
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int64 {
	t.Helper()
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	require.NoError(t, err)
	return count
}

func existsLiveSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, videoID string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM youtube_live_sessions WHERE video_id = $1)", videoID).Scan(&exists)
	require.NoError(t, err)
	return exists
}
