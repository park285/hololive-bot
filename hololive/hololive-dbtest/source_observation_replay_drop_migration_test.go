package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const sourceObservationReplayDropMigration = "348_drop_source_observation_replay.sql"

// 348은 PENDING replay 요청이 남아 있으면 아무것도 지우지 않고 실패하고, 끝난 요청만 있으면 표와 replay_count 열을 지운다.
func TestSourceObservationReplayDropRejectsPendingRequests(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, dir := projectionMigrationPoolBefore(t, sourceObservationReplayDropMigration)

	_, err := pool.Exec(ctx, `INSERT INTO source_observation_replay_requests
		(provider, observation_kind, subject_key, observation_key, evidence_sha256, requested_by, reason, previous_attempt_count)
		VALUES ('youtubejs', 'community_page', 'UC_TEST', 'key', repeat('a', 64), 'operator', 'pending', 0)`)
	require.NoError(t, err)

	err = applyMigrationFile(ctx, pool, dir, sourceObservationReplayDropMigration)
	require.ErrorContains(t, err, "PENDING rows")
	require.True(t, replayDropObjectsExist(t, pool), "failed drop must keep the request table and replay_count")

	_, err = pool.Exec(ctx, `UPDATE source_observation_replay_requests
		SET status = 'REJECTED', rejection_code = 'operator_closed'`)
	require.NoError(t, err)

	for _, attempt := range []string{"first", "reapply"} {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, sourceObservationReplayDropMigration), attempt)
		require.False(t, replayDropObjectsExist(t, pool), attempt)
	}
}

func replayDropObjectsExist(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()

	var table, column bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		to_regclass('public.source_observation_replay_requests') IS NOT NULL,
		EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'source_observation_queue' AND column_name = 'replay_count')`,
	).Scan(&table, &column))
	require.Equal(t, table, column, "table and replay_count must change together")

	return table
}
