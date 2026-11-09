package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCollectionRuntimeBundlesMigrationRuntimeACL(t *testing.T) {
	t.Parallel()

	pool := NewBlankPool(t)
	roles := createObservationGrantRoles(t, pool)
	grantBroadBaselineDefaults(t, pool, roles)
	applyBaselineForRoles(t, pool, roles)
	seedCollectionBundleHeaders(t, pool)

	assertCollectionBundleInternalACL(t, pool, roles)
	assertCollectionBundleRuntimeOperations(t, pool, roles.runtime)
}

func assertCollectionBundleInternalACL(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	for _, role := range []string{roles.runtime, roles.scraper} {
		var mappingRead, bundleRead, bundleWrite, guardWriter, lock bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			has_table_privilege($1,'youtube_collection_runtime_kinds','SELECT'),
			has_table_privilege($1,'youtube_collection_runtime_bundles','SELECT'),
			has_table_privilege($1,'youtube_collection_runtime_bundles','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'),
			has_column_privilege($1,'youtube_collection_projection_guard','runtime_writer','UPDATE'),
			has_function_privilege($1,'lock_youtube_collection_runtime_bundles()','EXECUTE')`, role).
			Scan(&mappingRead, &bundleRead, &bundleWrite, &guardWriter, &lock))
		require.Equal(t, role == roles.runtime, mappingRead)
		require.Equal(t, role == roles.runtime, bundleRead)
		require.False(t, bundleWrite)
		require.False(t, guardWriter)
		require.Equal(t, role == roles.runtime, lock)

		for _, function := range []string{
			"refresh_youtube_collection_runtime_bundles(bigint[])",
			"refresh_youtube_collection_runtime_bundle_keys(bigint[],text[],text[])",
			"guard_youtube_collection_runtime_bundles()",
			"refresh_youtube_collection_runtime_bundles_statement()",
		} {
			var allowed bool

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, function).Scan(&allowed))
			require.False(t, allowed, "%s must not execute %s", role, function)
		}
	}
}

func assertCollectionBundleRuntimeOperations(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	execCollectionBundleSQL(t, tx, `SET LOCAL ROLE `+pgx.Identifier{role}.Sanitize())
	execCollectionBundleSQL(t, tx, `SELECT lock_youtube_collection_runtime_bundles();
		SELECT guard_key FROM youtube_collection_projection_guard WHERE guard_key FOR UPDATE;
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
		VALUES (101,'runtime-writer','video_list',40,60000,true)`)

	var calls int

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT rpc_calls FROM youtube_collection_runtime_bundles
		WHERE projection_generation=101 AND kind='youtubejs_content' AND subject_key='runtime-writer'`).Scan(&calls))
	require.Equal(t, 1, calls)
	execCollectionBundleSQL(t, tx, `SELECT kind,observation_kind,rpc_per_kind FROM youtube_collection_runtime_kinds;
		SELECT deleted_reasons,deleted_targets,deleted_generations FROM delete_retired_youtube_projection_batch(now()-interval '1 day',1)`)

	for _, query := range []string{
		`DELETE FROM youtube_collection_runtime_bundles`,
		`UPDATE youtube_collection_projection_guard SET runtime_writer=pg_current_xact_id()`,
		`SELECT refresh_youtube_collection_runtime_bundles(ARRAY[101]::bigint[])`,
	} {
		require.NoError(t, expectObservationPermissionDenied(t.Context(), tx, query))
	}
}
