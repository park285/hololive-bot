package dbtest

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

func seedProtectedRetentionSet(t *testing.T, pool *pgxpool.Pool, base time.Time) ([]int64, []int64, []int64) {
	t.Helper()

	keys := []string{"queue", "head", "current", "staging", "leased", "older", "live", "upcoming", "pending-live", "pending-upcoming", "ended", "eligible"}
	ids := make([]int64, 0, len(keys))
	protected := make([]int64, 0, 9)
	eligible := make([]int64, 0, 3)

	for _, key := range keys {
		id := insertRetentionObservation(t, pool, communityPageKind, base, key)

		ids = append(ids, id)

		if key == "older" || key == "ended" || key == "eligible" {
			eligible = append(eligible, id)
		} else {
			protected = append(protected, id)
		}
	}

	_, err := pool.Exec(t.Context(), `
		INSERT INTO source_observation_queue(observation_id)
		SELECT id FROM source_observations WHERE subject_key='queue';
		INSERT INTO youtube_live_pending_ends
			(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
		SELECT 'set-head','UC_set','EXPLICIT_END',id,observed_at,received_at,scheduled_for,true,true
		FROM source_observations WHERE subject_key='head';
		INSERT INTO youtube_live_reconciliation_heads(video_id,status,end_candidate_kind,end_candidate_observation_id,next_end_check_at)
		SELECT 'set-head','LIVE','EXPLICIT_END',id,received_at FROM source_observations WHERE subject_key='head';
		INSERT INTO youtube_collection_projection_generations
			(generation,status,row_count,projection_sha256,valid_until,activated_at) OVERRIDING SYSTEM VALUE
		VALUES (9001,'CURRENT',0,repeat('a',64),now(),now()),
		       (9002,'STAGING',0,repeat('b',64),now(),NULL),
		       (9003,'RETIRED',0,repeat('c',64),now(),now());
		UPDATE source_observations SET projection_generation=CASE subject_key
			WHEN 'current' THEN 9001 WHEN 'staging' THEN 9002 ELSE 9003 END;
		INSERT INTO youtube_collection_job_leases
			(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
			 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at)
		VALUES ('leased','youtubejs','SUBJECT','community_collect','leased',9003,120000,
			'ACTIVE',now(),now(),'collector',now()+interval '1 minute');
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,lifecycle_origin)
		VALUES ('set-live','UC_set','LIVE','observed'), ('set-upcoming','UC_set','UPCOMING','metadata_only'),
		       ('set-pending-live','UC_set','LIVE','observed'), ('set-pending-upcoming','UC_set','UPCOMING','metadata_only'),
		       ('set-ended','UC_set','ENDED','observed');
		INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		SELECT id,source_observation_entity_kind_id('youtube_live_session'),
			CASE subject_key WHEN 'older' THEN 'set-live' ELSE 'set-'||subject_key END,
			source_observation_decision_id('APPLIED')
		FROM source_observations WHERE subject_key IN ('older','live','upcoming','ended');
		INSERT INTO youtube_live_pending_ends
			(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
		SELECT 'set-'||subject_key,'UC_set','EXPLICIT_END',id,observed_at,received_at,scheduled_for,true,true
		FROM source_observations WHERE subject_key IN ('pending-live','pending-upcoming','ended')`)
	require.NoError(t, err)

	return ids, protected, eligible
}

func TestSourceRetentionProtectedSetMatchesScalarAndDeletion(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	ids, protected, eligible := seedProtectedRetentionSet(t, pool, base)

	var setIDs, scalarIDs []int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(id ORDER BY id)
		FROM source_observation_retention_protected_ids($1)`, ids).Scan(&setIDs))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(id ORDER BY id)
		FROM source_observations WHERE source_observation_retention_protected(id)`).Scan(&scalarIDs))
	require.Equal(t, protected, setIDs, "여섯 보호 조건은 각각 독립적인 원본을 보존한다")
	require.Equal(t, protected, scalarIDs)

	var duplicateIDs []int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(id) FROM
		source_observation_retention_protected_ids(ARRAY[$1,$1,NULL,0]::bigint[])`, protected[0]).Scan(&duplicateIDs))
	require.Equal(t, protected[:1], duplicateIDs)

	var (
		emptyCount    int
		nullProtected bool
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM source_observation_retention_protected_ids(NULL::bigint[])),
		source_observation_retention_protected(NULL::bigint)`).Scan(&emptyCount, &nullProtected))
	require.Zero(t, emptyCount)
	require.False(t, nullProtected)

	var (
		deleted   []int64
		passed    int
		completed bool
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT deleted_ids,passed,traversal_completed
		FROM delete_source_observation_retention_window($1,$2,1000)`,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}).Scan(&deleted, &passed, &completed))
	require.ElementsMatch(t, eligible, deleted)
	require.Equal(t, len(ids), passed)
	require.True(t, completed)
	assertRetentionObservationsExist(t, pool, protected)
}

func TestSourceRetentionProtectedSetSkipsLocksAndAdvancesCursor(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 1002, 1)
	holder, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackRetentionTransaction(t, holder)

	_, err = holder.Exec(t.Context(), `SELECT id FROM source_observations ORDER BY received_at,id LIMIT 1000 FOR UPDATE`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var (
		deleted   []int64
		passed    int
		completed bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT deleted_ids,passed,traversal_completed
		FROM delete_source_observation_retention_window($1,$2,1)`,
		[]string{communityPageKind}, []time.Time{base.Add(time.Hour)}).Scan(&deleted, &passed, &completed))
	require.Empty(t, deleted)
	require.Equal(t, 1000, passed)
	require.False(t, completed)
	require.Equal(t, retentionWindow{deleted: 1, passed: 1}, deleteRetentionWindow(t, pool, base.Add(time.Hour), 1))
	require.Equal(t, retentionWindow{deleted: 1, passed: 1, completed: true}, deleteRetentionWindow(t, pool, base.Add(time.Hour), 1))
	require.NoError(t, pgxdb.Rollback(ctx, holder))
	require.Equal(t, retentionWindow{deleted: 999, passed: 1000, completed: true}, deleteRetentionWindow(t, pool, base.Add(time.Hour), 1000))
	requireRetentionCount(t, pool, 1)
}

func TestSourceRetentionProtectedSetRuntimePrivileges(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	roles := createObservationGrantRoles(t, pool)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	files, err := fs.Glob(os.DirFS(dir), "*_source_retention_protected_set.sql")
	require.NoError(t, err)
	require.Len(t, files, 1)

	raw, err := fs.ReadFile(os.DirFS(dir), files[0])
	require.NoError(t, err)

	migration := strings.ReplaceAll(string(raw), "hololive_runtime", roles.runtime)

	for range 2 {
		require.NoError(t, applyMigrationContent(t.Context(), pool, files[0], migration))
	}

	_, err = pool.Exec(t.Context(), `GRANT SELECT ON source_observations,source_observation_queue,
		youtube_live_reconciliation_heads,youtube_collection_projection_generations,youtube_collection_job_leases,
		source_observation_provenances,source_observation_applications,source_observation_entity_kinds,
		youtube_live_sessions,youtube_live_pending_ends TO `+pgx.Identifier{roles.runtime}.Sanitize())
	require.NoError(t, err)

	for _, role := range []string{roles.runtime, roles.scraper} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			tx, beginErr := pool.Begin(t.Context())
			require.NoError(t, beginErr)

			defer rollbackRetentionTransaction(t, tx)

			_, roleErr := tx.Exec(t.Context(), `SET LOCAL ROLE `+pgx.Identifier{role}.Sanitize())
			require.NoError(t, roleErr)

			var protected bool

			callErr := tx.QueryRow(t.Context(), `SELECT source_observation_retention_protected(0)`).Scan(&protected)

			if role == roles.scraper {
				require.ErrorContains(t, callErr, "SQLSTATE 42501")

				return
			}

			require.NoError(t, callErr)
			require.False(t, protected)

			var completed bool

			require.NoError(t, tx.QueryRow(t.Context(), `SELECT traversal_completed
				FROM delete_source_observation_retention_window(ARRAY['community_page'],ARRAY[now()],1)`).Scan(&completed))
			require.True(t, completed)
		})
	}
}
