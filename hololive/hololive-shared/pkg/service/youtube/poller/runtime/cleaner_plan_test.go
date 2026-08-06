package polling

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestViewerSampleCleanerDeleteBatchSkipsLockedSession(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()

	insertViewerSampleCleanerLiveSession(t, ctx, pool, "a-locked", domain.LiveStatusEnded, now.AddDate(0, 0, -9))
	insertViewerSampleCleanerLiveSession(t, ctx, pool, "b-available", domain.LiveStatusEnded, now.AddDate(0, 0, -8))
	insertViewerSampleCleanerSample(t, ctx, pool, "a-locked", now)
	insertViewerSampleCleanerSample(t, ctx, pool, "b-available", now)

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = holder.Rollback(context.Background())
	})
	_, err = holder.Exec(ctx, "SELECT video_id FROM youtube_live_sessions WHERE video_id = $1 FOR UPDATE", "a-locked")
	require.NoError(t, err)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: 1})
	step, err := cleaner.deleteNextBatch(ctx, pool, now.AddDate(0, 0, -7), initialViewerSampleCleanupCursor())
	require.NoError(t, err)
	require.EqualValues(t, 1, step.deleted)
	require.NotNil(t, step.target)
	require.Equal(t, "b-available", step.target.videoID)
	require.EqualValues(t, 1, countViewerSampleCleanerSamples(t, ctx, pool, "a-locked"))
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, ctx, pool, "b-available"))
}

func TestViewerSampleCleanerPlanBoundsIneligiblePrefixAndPaginatesEmptySessions(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	oldEndedAt := now.AddDate(0, 0, -30)
	const (
		ineligibleVideoID  = "a-ineligible"
		eligibleVideoID    = "z-eligible"
		ineligibleSamples  = 10_000
		emptyEndedSessions = 130
		liveSessions       = 2_000
		batchSize          = 2
	)

	insertViewerSampleCleanerLiveSession(t, ctx, pool, ineligibleVideoID, domain.LiveStatusLive, oldEndedAt)
	insertViewerSampleCleanerLiveSession(t, ctx, pool, eligibleVideoID, domain.LiveStatusEnded, oldEndedAt)
	insertViewerSampleCleanerSessions(t, ctx, pool, "empty-", emptyEndedSessions, domain.LiveStatusEnded, oldEndedAt)
	insertViewerSampleCleanerSessions(t, ctx, pool, "live-", liveSessions, domain.LiveStatusLive, oldEndedAt)
	insertViewerSampleCleanerSamples(t, ctx, pool, ineligibleVideoID, now, ineligibleSamples)
	for i := range 3 {
		insertViewerSampleCleanerSample(t, ctx, pool, eligibleVideoID, now.Add(time.Duration(i)*time.Second))
	}
	_, err := pool.Exec(ctx, "ANALYZE youtube_live_sessions, youtube_live_viewer_samples")
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
	})

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = force_generic_plan")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `PREPARE viewer_sample_cleanup_plan(
		timestamptz, timestamptz, varchar(20), integer, integer
	) AS `+mustSQL("cleaner_0176_03.sql"))
	require.NoError(t, err)

	var rawPlan string
	explainSQL := `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
		EXECUTE viewer_sample_cleanup_plan(
			CURRENT_TIMESTAMP - INTERVAL '7 days',
			TIMESTAMPTZ '0001-01-01 00:00:00+00',
			''::varchar(20),
			64,
			2
		)`
	err = tx.QueryRow(ctx, explainSQL).Scan(&rawPlan)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "DEALLOCATE viewer_sample_cleanup_plan")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	var plans []viewerSampleExplainEnvelope
	require.NoError(t, json.Unmarshal([]byte(rawPlan), &plans))
	require.Len(t, plans, 1)
	require.True(t, viewerSamplePlanUsesIndex(plans[0].Plan, "idx_yls_ended_cleanup"))
	require.True(t, viewerSamplePlanUsesIndex(plans[0].Plan, "youtube_live_viewer_samples_pkey"))
	require.False(t, viewerSamplePlanHasSequentialSampleScan(plans[0].Plan))
	require.LessOrEqual(
		t,
		viewerSamplePlanMaxExaminedRows(plans[0].Plan),
		float64(viewerSampleCleanupSessionPageSize*2),
	)

	cleaner := NewViewerSampleCleaner(pool, ViewerSampleCleanerConfig{RetentionDays: 7, BatchSize: batchSize})
	cleaner.maxBatches = 8
	cleaner.maxDuration = time.Minute
	deleted, err := cleaner.Cleanup(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted)
	require.EqualValues(t, ineligibleSamples, countViewerSampleCleanerSamples(t, ctx, pool, ineligibleVideoID))
	require.EqualValues(t, 0, countViewerSampleCleanerSamples(t, ctx, pool, eligibleVideoID))
}

type viewerSampleExplainEnvelope struct {
	Plan viewerSampleExplainNode `json:"Plan"`
}

type viewerSampleExplainNode struct {
	NodeType                  string                    `json:"Node Type"`
	ActualLoops               float64                   `json:"Actual Loops"`
	ActualRows                float64                   `json:"Actual Rows"`
	IndexName                 string                    `json:"Index Name"`
	RelationName              string                    `json:"Relation Name"`
	RowsRemovedByFilter       float64                   `json:"Rows Removed by Filter"`
	RowsRemovedByIndexRecheck float64                   `json:"Rows Removed by Index Recheck"`
	Plans                     []viewerSampleExplainNode `json:"Plans"`
}

func viewerSamplePlanUsesIndex(node viewerSampleExplainNode, indexName string) bool {
	if node.IndexName == indexName {
		return true
	}
	for _, child := range node.Plans {
		if viewerSamplePlanUsesIndex(child, indexName) {
			return true
		}
	}
	return false
}

func viewerSamplePlanHasSequentialSampleScan(node viewerSampleExplainNode) bool {
	if node.RelationName == "youtube_live_viewer_samples" && node.NodeType == "Seq Scan" {
		return true
	}
	for _, child := range node.Plans {
		if viewerSamplePlanHasSequentialSampleScan(child) {
			return true
		}
	}
	return false
}

func viewerSamplePlanMaxExaminedRows(node viewerSampleExplainNode) float64 {
	maxRows := float64(0)
	if node.RelationName == "youtube_live_viewer_samples" || node.IndexName == "youtube_live_viewer_samples_pkey" {
		loops := node.ActualLoops
		if loops < 1 {
			loops = 1
		}
		maxRows = (node.ActualRows + node.RowsRemovedByFilter + node.RowsRemovedByIndexRecheck) * loops
	}
	for _, child := range node.Plans {
		maxRows = max(maxRows, viewerSamplePlanMaxExaminedRows(child))
	}
	return maxRows
}

func insertViewerSampleCleanerSessions(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	prefix string,
	count int,
	status domain.LiveStatus,
	endedAt time.Time,
) {
	t.Helper()
	width := 20 - len(prefix)
	require.Positive(t, width)
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, ended_at)
		SELECT
			$1 || lpad(g::text, $2::integer, '0'),
			'channel-' || $1 || lpad(g::text, $2::integer, '0'),
			$3,
			$4
		FROM generate_series(0, $5::integer - 1) AS g`,
		prefix,
		width,
		status,
		endedAt,
		count,
	)
	require.NoError(t, err)
}

func insertViewerSampleCleanerSamples(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	videoID string,
	capturedAt time.Time,
	count int,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_viewer_samples (video_id, captured_at, channel_id, concurrent_viewers)
		SELECT $1, $2::timestamptz + g * interval '1 microsecond', $3, 100
		FROM generate_series(1, $4::integer) AS g`,
		videoID,
		capturedAt,
		"channel-"+videoID,
		count,
	)
	require.NoError(t, err)
}
