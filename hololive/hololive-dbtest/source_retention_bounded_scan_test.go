package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

func TestSourceRetentionProgressesPastProtectedPrefixAndRevisits(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 11000, 10000)
	kinds, cutoffs := []string{communityPageKind}, []time.Time{base.Add(time.Hour)}

	for tick := range 11 {
		deleted := deleteRetentionBatch(t, pool, kinds, cutoffs, 1000)

		if tick < 10 {
			require.Empty(t, deleted, "protected prefix tick %d", tick)
		} else {
			require.Len(t, deleted, 1000, "eligible tail must make finite progress")
		}
	}

	_, err := pool.Exec(t.Context(), `DELETE FROM source_observation_queue`)
	require.NoError(t, err)

	for range 10 {
		require.Len(t, deleteRetentionBatch(t, pool, kinds, cutoffs, 1000), 1000)
	}

	requireRetentionCount(t, pool, 0)
}

func TestSourceRetentionBoundsProtectionChecksAndRollsBackCursor(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 11000, 10000)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackRetentionTransaction(t, tx)

	_, err = tx.Exec(t.Context(), `SET LOCAL track_functions = 'all'`)
	require.NoError(t, err)

	var deleted int

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM delete_source_observation_retention_batch($1,$2,1000)`,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}).Scan(&deleted))
	require.Zero(t, deleted)

	var calls, cursor int64

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT calls FROM pg_stat_xact_user_functions
		WHERE funcid='source_observation_retention_protected(bigint)'::regprocedure`).Scan(&calls))
	require.EqualValues(t, 1000, calls, "only the bounded window may undergo protection checks")
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT cursor_id FROM source_observation_retention_state`).Scan(&cursor))
	require.Positive(t, cursor)
	require.NoError(t, pgxdb.Rollback(t.Context(), tx))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT cursor_id FROM source_observation_retention_state`).Scan(&cursor))
	require.Zero(t, cursor, "rollback must not lose the uncommitted scan window")
	requireRetentionCount(t, pool, 11000)

	// 새 연결도 같은 영속 커서를 이어받으며 다시 첫 1000개만 검사한다.
	require.Empty(t, deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1000))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT cursor_id FROM source_observation_retention_state`).Scan(&cursor))
	require.Positive(t, cursor)
}

func TestSourceRetentionBoundsLockedWindowAndRevisits(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 1001, 0)
	holder, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackRetentionTransaction(t, holder)

	_, err = holder.Exec(t.Context(), `SELECT id FROM source_observations ORDER BY received_at,id LIMIT 1000 FOR UPDATE`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	kinds, cutoffs := []string{communityPageKind}, []time.Time{base.Add(time.Hour)}
	require.Empty(t, deleteRetentionBatchContext(ctx, t, pool, kinds, cutoffs, 1))
	require.Len(t, deleteRetentionBatchContext(ctx, t, pool, kinds, cutoffs, 1), 1)
	require.NoError(t, pgxdb.Rollback(ctx, holder))
	require.Len(t, deleteRetentionBatchContext(ctx, t, pool, kinds, cutoffs, 1000), 1000)
	requireRetentionCount(t, pool, 0)
}

func TestSourceRetentionFixedSweepRevisitsDespiteNewTail(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 1001, 1000)
	kinds := []string{communityPageKind, "live_snapshot"}
	cutoffs := []time.Time{base.Add(time.Hour), base.Add(time.Hour)}
	require.Empty(t, deleteRetentionBatch(t, pool, kinds, cutoffs, 1))

	late := insertRetentionObservation(t, pool, "live_snapshot", base.Add(time.Minute), "new-tail")
	_, err := pool.Exec(t.Context(), `DELETE FROM source_observation_queue`)
	require.NoError(t, err)
	require.Len(t, deleteRetentionBatch(t, pool, kinds, cutoffs, 1), 1)
	assertRetentionObservationsExist(t, pool, []int64{late})
	// 최초 순회 상한까지 완료되면 새 tail보다 앞의 해제된 prefix가 먼저 재방문된다.
	require.Len(t, deleteRetentionBatch(t, pool, kinds, cutoffs, 1000), 1000)
	require.Equal(t, []int64{late}, deleteRetentionBatch(t, pool, kinds, cutoffs, 1000))
	requireRetentionCount(t, pool, 0)
}

func TestSourceRetentionChildCascadeIsAtomic(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 1, 0)
	_, err := pool.Exec(t.Context(), `INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		SELECT o.id,source_observation_entity_kind_id('retention_fanout'),s::text,source_observation_decision_id('APPLIED')
		FROM source_observations o CROSS JOIN generate_series(1,10000) s`)
	require.NoError(t, err)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackRetentionTransaction(t, tx)

	var deleted, children int

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM delete_source_observation_retention_batch($1,$2,1)`,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}).Scan(&deleted))
	require.Equal(t, 1, deleted)
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications`).Scan(&children))
	require.Zero(t, children)
	require.NoError(t, pgxdb.Rollback(t.Context(), tx))
	requireRetentionCount(t, pool, 1)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications`).Scan(&children))
	require.Equal(t, 10000, children)
	require.Len(t, deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1), 1)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications`).Scan(&children))
	require.Zero(t, children)
}

func TestSourceRetentionRevisitsAfterPolicyChangeAndMissingHorizon(t *testing.T) {
	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 1001, 1000)
	cutoff := base.Add(time.Hour)
	require.Empty(t, deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{cutoff}, 1))

	// 진행 커서 뒤의 순회 상한이 사라지고, 앞쪽에 새 kind의 오래된 원본이 생긴다.
	_, err := pool.Exec(t.Context(), `DELETE FROM source_observations WHERE observation_key=sha256('bounded-1001'::bytea)`)
	require.NoError(t, err)

	lateID := insertRetentionObservation(t, pool, "live_snapshot", base.Add(-time.Hour), "policy-late")
	recentID := insertRetentionObservation(t, pool, "live_snapshot", base.Add(2*time.Hour), "policy-recent")
	require.Empty(t, deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(-time.Hour)}, 1000))

	_, err = pool.Exec(t.Context(), `DELETE FROM source_observation_queue`)
	require.NoError(t, err)

	// cutoff와 활성 kind 집합이 다시 넓어져도 앞쪽을 잃지 않고 최신 원본은 보존한다.
	kinds := []string{communityPageKind, "live_snapshot"}
	cutoffs := []time.Time{cutoff, cutoff}
	first := deleteRetentionBatch(t, pool, kinds, cutoffs, 1000)
	require.Len(t, first, 1000)
	require.Contains(t, first, lateID)
	require.Len(t, deleteRetentionBatch(t, pool, kinds, cutoffs, 1000), 1)
	assertRetentionObservationsExist(t, pool, []int64{recentID})
	requireRetentionCount(t, pool, 1)
}

func seedBoundedRetention(t *testing.T, pool *pgxpool.Pool, count, protected int) time.Time {
	t.Helper()

	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	_, err := pool.Exec(t.Context(), `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	) INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
		scheduled_for,observed_at,received_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
		provenance_id, fence_epoch,projection_generation)
	SELECT 'youtubejs','community_page','bounded-'||s,sha256(('bounded-'||s)::bytea),1,1,$1,$1,$1::timestamptz+s*interval '1 microsecond',
		decode(repeat('a',64),'hex'),'COMPLETE','CONTIGUOUS',payload.id,decode(repeat('c',64),'hex'),
		source_observation_provenance_id('bounded-'||s,'dbtest','community_page'),1,1 FROM payload CROSS JOIN generate_series(1,$2::int) s`, base, count)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `INSERT INTO source_observation_queue(observation_id)
		SELECT id FROM source_observations ORDER BY received_at,id LIMIT $1`, protected)
	require.NoError(t, err)

	return base
}

func requireRetentionCount(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observations`).Scan(&count))
	require.Equal(t, want, count)
}

func rollbackRetentionTransaction(t *testing.T, tx pgx.Tx) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)

	defer cancel()

	if err := pgxdb.Rollback(ctx, tx); err != nil {
		t.Errorf("rollback retention transaction: %v", err)
	}
}
