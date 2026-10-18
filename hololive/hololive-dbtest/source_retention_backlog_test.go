package dbtest

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const (
	retentionBacklogQueryPath = "../hololive-api/internal/youtube/sourceobservation/queries/repository_retention_backlog_evidence.sql"
	retentionLiveSnapshotKind = "live_snapshot"
)

type retentionBacklogMeasurement struct {
	oldest *time.Time
	known  *bool
}

func TestSourceRetentionBacklogBoundedEvidence(t *testing.T) {
	sql := readRetentionBacklogSQL(t)

	for _, test := range []struct {
		name      string
		rows      int
		protected int
		limit     int
		known     bool
		oldestRow int
		calls     int64
	}{
		{name: "empty kind", limit: 4, known: true},
		{name: "short protected window", rows: 3, protected: 3, limit: 4, known: true, calls: 3},
		{name: "exact protected window", rows: 4, protected: 4, limit: 4, calls: 4},
		{name: "eligible beyond window remains unknown", rows: 5, protected: 4, limit: 4, calls: 4},
		{name: "first eligible stops checks", rows: 1000, limit: 1000, known: true, oldestRow: 1, calls: 1},
		{name: "protected prefix", rows: 1000, protected: 3, limit: 1000, known: true, oldestRow: 4, calls: 4},
		{name: "eligible at window boundary", rows: 5, protected: 3, limit: 4, known: true, oldestRow: 4, calls: 4},
		{name: "zero window", rows: 1, limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := NewPool(t)
			base := seedBoundedRetention(t, pool, test.rows, test.protected)
			tx := beginRetentionMeasurement(t, pool)
			got, calls := measureRetentionBacklog(t, tx, sql,
				[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}, test.limit)
			require.Equal(t, new(test.known), got.known)

			if test.oldestRow == 0 {
				require.Nil(t, got.oldest)
			} else {
				require.NotNil(t, got.oldest)
				require.WithinDuration(t, base.Add(time.Duration(test.oldestRow)*time.Microsecond), *got.oldest, 0)
			}

			require.Equal(t, test.calls, calls, "only the sorted prefix through the first eligible row needs protection checks")
		})
	}
}

func TestSourceRetentionBacklogCombinesKindsAndCutoffs(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 4, 4)
	insertRetentionObservation(t, pool, retentionLiveSnapshotKind, base.Add(-time.Hour), "backlog-other-kind")
	insertRetentionObservation(t, pool, retentionLiveSnapshotKind, base.Add(time.Hour), "backlog-at-cutoff")

	sql := readRetentionBacklogSQL(t)
	tx := beginRetentionMeasurement(t, pool)

	// One saturated protected kind makes the combined result unknown, even when
	// another kind has a known oldest row. Empty kinds do not change that result.
	got, calls := measureRetentionBacklog(t, tx, sql,
		[]string{communityPageKind, retentionLiveSnapshotKind, "live_check"},
		[]time.Time{base.Add(time.Hour), base.Add(time.Hour), base.Add(time.Hour)}, 4)
	require.NotNil(t, got.oldest)
	require.WithinDuration(t, base.Add(-time.Hour), *got.oldest, 0)
	require.Equal(t, new(false), got.known)
	require.EqualValues(t, 5, calls)

	// The cutoff is exclusive: an otherwise eligible row at exactly the cutoff
	// is not evidence of a backlog, nor does it fill the bounded window.
	got, calls = measureRetentionBacklog(t, tx, sql,
		[]string{retentionLiveSnapshotKind}, []time.Time{base.Add(-time.Hour)}, 1)
	require.Nil(t, got.oldest)
	require.Equal(t, new(true), got.known)
	require.Zero(t, calls)

	got, calls = measureRetentionBacklog(t, tx, sql, []string{}, []time.Time{}, 4)
	require.Nil(t, got.oldest)
	require.Nil(t, got.known, "empty policy aggregation preserves SQL NULL")
	require.Zero(t, calls)

	// Duplicate policy inputs retain the original per-kind aggregate count.
	got, _ = measureRetentionBacklog(t, tx, sql,
		[]string{communityPageKind, communityPageKind},
		[]time.Time{base.Add(time.Hour), base.Add(time.Hour)}, 5)
	require.Nil(t, got.oldest)
	require.Equal(t, new(false), got.known)
}

func TestSourceRetentionBacklogOrdersTiesByID(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 3, 1)
	_, err := pool.Exec(t.Context(), `UPDATE source_observations SET received_at=$1`, base)
	require.NoError(t, err)

	tx := beginRetentionMeasurement(t, pool)
	sql := readRetentionBacklogSQL(t)

	got, calls := measureRetentionBacklog(t, tx, sql,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1)
	require.Nil(t, got.oldest)
	require.Equal(t, new(false), got.known)
	require.EqualValues(t, 1, calls)

	got, calls = measureRetentionBacklog(t, tx, sql,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 2)
	require.NotNil(t, got.oldest)
	require.WithinDuration(t, base, *got.oldest, 0)
	require.Equal(t, new(true), got.known)
	require.EqualValues(t, 2, calls)
}

func readRetentionBacklogSQL(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(retentionBacklogQueryPath)
	require.NoError(t, err)

	return string(raw)
}

func beginRetentionMeasurement(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pgxdb.Rollback(t.Context(), tx))
	})

	_, err = tx.Exec(t.Context(), `SET LOCAL track_functions = 'all'`)
	require.NoError(t, err)

	return tx
}

func retentionProtectionCalls(t *testing.T, tx pgx.Tx) int64 {
	t.Helper()

	var calls int64

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT coalesce(sum(calls),0)::bigint
		FROM pg_stat_xact_user_functions
		WHERE funcid='source_observation_retention_protected(bigint)'::regprocedure`).Scan(&calls))

	return calls
}

func measureRetentionBacklog(t *testing.T, tx pgx.Tx, sql string, kinds []string, cutoffs []time.Time, limit int) (retentionBacklogMeasurement, int64) {
	t.Helper()

	before := retentionProtectionCalls(t, tx)

	var got retentionBacklogMeasurement

	require.NoError(t, tx.QueryRow(t.Context(), sql, kinds, cutoffs, limit).Scan(&got.oldest, &got.known))

	return got, retentionProtectionCalls(t, tx) - before
}
