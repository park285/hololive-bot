package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCollectionRuntimeBundlesMigrationBackfillAndReapply(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "empty"

		if populated {
			name = "populated"
		}

		t.Run(name, func(t *testing.T) {
			pool, dir := projectionMigrationPoolBefore(t, collectionRuntimeBundlesMigration)

			if populated {
				seedCollectionBundleHeaders(t, pool)
				seedCollectionBundleTargets(t, pool)
			}

			before := projectionUpgradeSnapshot(t, pool)

			for range 2 {
				require.NoError(t, applyMigrationFile(t.Context(), pool, dir, collectionRuntimeBundlesMigration))
				assertCollectionRuntimeBundles(t, pool)
				require.Equal(t, before, projectionUpgradeSnapshot(t, pool), "backfill은 원본 tuple이나 기존 함수 권한을 바꾸지 않습니다")
			}
		})
	}
}

func prepareCollectionBundleBroadDefaults(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	runtime := pgx.Identifier{roles.runtime}.Sanitize()
	scraper := pgx.Identifier{roles.scraper}.Sanitize()

	// init-db의 migrator 기본 권한을 재현하여 새 SECURITY DEFINER 함수가 런타임에 새지 않게 한다.
	execCollectionBundleSQL(t, pool, `ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO `+runtime+`, `+scraper+`;
		ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO `+runtime+`, `+scraper+`;
		GRANT SELECT,UPDATE ON youtube_collection_projection_guard TO `+runtime+`;
		GRANT EXECUTE ON FUNCTION delete_retired_youtube_projection_batch(timestamptz,integer) TO `+runtime+`;
		GRANT EXECUTE ON FUNCTION lock_current_youtube_collection_projection() TO `+scraper+`;
		GRANT INSERT,SELECT,UPDATE,DELETE ON youtube_collection_targets TO `+runtime+`;
		GRANT SELECT ON youtube_collection_projection_generations TO `+runtime)
}

func TestCollectionRuntimeBundlesMigrationRuntimeACL(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, collectionRuntimeBundlesMigration)
	roles := createObservationGrantRoles(t, pool)
	prepareCollectionBundleBroadDefaults(t, pool, roles)
	seedCollectionBundleHeaders(t, pool)

	raw, err := fs.ReadFile(os.DirFS(dir), collectionRuntimeBundlesMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))

	for range 2 {
		require.NoError(t, applyMigrationContent(t.Context(), pool, collectionRuntimeBundlesMigration, migration))
	}

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
