package sourceobservation

import (
	"slices"
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
	history := toastIgnoredHistory(at)
	state := live.SessionState{VideoID: "head-toast", Status: testStatusUpcoming, IgnoredAbsences: live.LoadedIgnoredAbsences(history)}

	require.EqualValues(t, 1, execLiveHead(t, pool, &state))

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before, "fixture must use external TOAST storage")

	state.Clock.LastUpcomingPositiveAt = &at

	require.EqualValues(t, 1, execLiveHead(t, pool, &state))
	require.Equal(t, before, liveHeadToastIDs(t, pool), "scalar changes must reuse equal array storage")

	var storedAt time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_upcoming_positive_at FROM youtube_live_reconciliation_heads WHERE video_id=$1`, state.VideoID).Scan(&storedAt))
	require.True(t, storedAt.Equal(at))
	require.Zero(t, execLiveHead(t, pool, &state), "identical replay must remain a no-op")

	appended := append(slices.Clone(history), at.Add(17000*time.Minute))

	state.IgnoredAbsences = live.LoadedIgnoredAbsences(appended)

	require.EqualValues(t, 1, execLiveHead(t, pool, &state))
	require.NotEqual(t, before, liveHeadToastIDs(t, pool), "changed arrays must be persisted")
	require.True(t, storedIgnoredEquals(t, pool, state.VideoID, appended))

	state.IgnoredAbsences = live.LoadedIgnoredAbsences(nil)

	require.EqualValues(t, 1, execLiveHead(t, pool, &state))
	require.True(t, storedIgnoredEquals(t, pool, state.VideoID, []time.Time{}), "loaded empty history must clear the array")
}

// 이력을 적재하지 않은 세션의 저장은 배열을 지우거나 다시 쓰지 않는다. 새 head는 빈 배열로 시작한다.
func TestLiveHeadOmittedHistoryKeepsStoredArray(t *testing.T) {
	pool := dbtest.NewPool(t)
	at := time.Date(2026, time.October, 6, 0, 0, 0, 0, time.UTC)
	history := toastIgnoredHistory(at)
	state := live.SessionState{VideoID: "head-omit", Status: testStatusEnded, IgnoredAbsences: live.LoadedIgnoredAbsences(history)}

	require.EqualValues(t, 1, execLiveHead(t, pool, &state))

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before, "fixture must use external TOAST storage")

	state.IgnoredAbsences = live.IgnoredAbsenceHistory{}

	require.Zero(t, execLiveHead(t, pool, &state), "omitted history with unchanged scalars must remain a no-op")

	state.Clock.LastEndEvidenceAt = &at

	require.EqualValues(t, 1, execLiveHead(t, pool, &state), "omitted history must still persist scalar changes")
	require.Equal(t, before, liveHeadToastIDs(t, pool), "omitted history must keep the stored TOAST value")
	require.True(t, storedIgnoredEquals(t, pool, state.VideoID, history), "omitted history erased stored history")

	fresh := live.SessionState{VideoID: "head-fresh", Status: testStatusUpcoming}

	require.EqualValues(t, 1, execLiveHead(t, pool, &fresh))
	require.True(t, storedIgnoredEquals(t, pool, fresh.VideoID, []time.Time{}), "a new head without loaded history starts empty")
}

func toastIgnoredHistory(at time.Time) []time.Time {
	history := make([]time.Time, 17000)

	for i := range history {
		history[i] = at.Add(time.Duration(i) * time.Minute)
	}

	return history
}

func execLiveHead(t *testing.T, pool *pgxpool.Pool, state *live.SessionState) int64 {
	t.Helper()

	statement := liveHeadStatement(state)

	tag, err := pool.Exec(t.Context(), statement.SQL, statement.Args...)
	require.NoError(t, err)

	return tag.RowsAffected()
}

func storedIgnoredEquals(t *testing.T, pool *pgxpool.Pool, videoID string, want []time.Time) bool {
	t.Helper()

	var equal bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT ignored_absence_scheduled_for=$2::timestamptz[] FROM youtube_live_reconciliation_heads WHERE video_id=$1`, videoID, want).Scan(&equal))

	return equal
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
