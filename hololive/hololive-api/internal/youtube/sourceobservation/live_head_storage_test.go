package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestLiveHeadPreservesUnchangedToastArray(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	at := time.Date(2026, time.October, 6, 0, 0, 0, 0, time.UTC)
	state := live.SessionState{VideoID: "head-toast", Status: "UPCOMING"}

	state.IgnoredAbsenceScheduledFor = make([]time.Time, 17000)

	for i := range state.IgnoredAbsenceScheduledFor {
		state.IgnoredAbsenceScheduledFor[i] = at.Add(time.Duration(i) * time.Minute)
	}

	statement := liveHeadStatement(&state)
	_, err := pool.Exec(ctx, statement.SQL, statement.Args...)
	require.NoError(t, err)

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before, "fixture must use external TOAST storage")

	state.Clock.LastUpcomingPositiveAt = &at
	statement = liveHeadStatement(&state)

	tag, err := pool.Exec(ctx, statement.SQL, statement.Args...)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	require.Equal(t, before, liveHeadToastIDs(t, pool), "scalar changes must reuse equal array storage")

	var storedAt time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_upcoming_positive_at FROM youtube_live_reconciliation_heads WHERE video_id=$1`, state.VideoID).Scan(&storedAt))
	require.True(t, storedAt.Equal(at))

	tag, err = pool.Exec(ctx, statement.SQL, statement.Args...)
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected(), "identical replay must remain a no-op")

	state.IgnoredAbsenceScheduledFor = append(state.IgnoredAbsenceScheduledFor, at.Add(17000*time.Minute))
	statement = liveHeadStatement(&state)
	_, err = pool.Exec(ctx, statement.SQL, statement.Args...)
	require.NoError(t, err)
	require.NotEqual(t, before, liveHeadToastIDs(t, pool), "changed arrays must be persisted")

	var equal bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT ignored_absence_scheduled_for=$2 FROM youtube_live_reconciliation_heads WHERE video_id=$1`, state.VideoID, state.IgnoredAbsenceScheduledFor).Scan(&equal))
	require.True(t, equal)

	state.IgnoredAbsenceScheduledFor = nil
	statement = liveHeadStatement(&state)
	_, err = pool.Exec(ctx, statement.SQL, statement.Args...)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT ignored_absence_scheduled_for='{}' FROM youtube_live_reconciliation_heads WHERE video_id=$1`, state.VideoID).Scan(&equal))
	require.True(t, equal, "nil input must still clear the array")
}

func liveHeadToastIDs(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()

	var schema, table string

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT namespace.nspname, toast.relname
		FROM pg_class head JOIN pg_class toast ON toast.oid=head.reltoastrelid
		JOIN pg_namespace namespace ON namespace.oid=toast.relnamespace
		WHERE head.oid='youtube_live_reconciliation_heads'::regclass`).Scan(&schema, &table))

	var ids []int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(DISTINCT chunk_id::bigint ORDER BY chunk_id::bigint) FROM `+pgx.Identifier{schema, table}.Sanitize()).Scan(&ids))

	return ids
}
