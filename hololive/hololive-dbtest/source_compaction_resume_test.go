package dbtest

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCompactionBackfillResumesCommittedBatches(t *testing.T) {
	for _, scenario := range []struct{ name, migration, table, column, seed string }{
		{"provenance", provenanceCompactionMigration, sourceObservationsTable, "provenance_id", `INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
   scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
   SELECT provider,observation_kind,'resume-'||n,encode(sha256(('resume-'||n)::bytea),'hex'),schema_version,contract_generation,
   scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation
   FROM source_observations CROSS JOIN generate_series(1,10000) n WHERE subject_key='leased'`},
		{"vocabulary", applicationVocabMigration, "source_observation_applications", "entity_kind_id", `INSERT INTO source_observation_applications(observation_id,entity_kind,entity_key,decision)
   SELECT id,'community_window','resume-'||n,'CANONICALIZED' FROM source_observations CROSS JOIN generate_series(1,10000) n WHERE subject_key='leased'`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pool, dir := projectionMigrationPoolBefore(t, provenanceCompactionMigration)
			seedProvenanceCutoverRows(t, pool)

			if scenario.migration == applicationVocabMigration {
				require.NoError(t, applyMigrationFile(t.Context(), pool, dir, provenanceCompactionMigration))
			}

			_, err := pool.Exec(t.Context(), scenario.seed)
			require.NoError(t, err)

			table := pgx.Identifier{"public", scenario.table}.Sanitize()
			column := pgx.Identifier{scenario.column}.Sanitize()

			var total, lastID int64

			require.NoError(t, pool.QueryRow(t.Context(), fmt.Sprintf("SELECT count(id),max(id) FROM %s", table)).Scan(&total, &lastID))
			installCompactionLastRowFailure(t, pool, table, lastID)
			require.Error(t, applyMigrationFile(t.Context(), pool, dir, scenario.migration))

			var exists bool

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=$1::regclass AND attname=$2 AND NOT attisdropped)`, table, scenario.column).Scan(&exists))
			require.True(t, exists, "후속 batch 실패가 준비 단계까지 되돌려서는 안 된다")

			var completed, remaining int64

			require.NoError(t, pool.QueryRow(t.Context(), fmt.Sprintf("SELECT count(id) FILTER (WHERE %s IS NOT NULL),count(id) FILTER (WHERE %s IS NULL) FROM %s", column, column, table)).Scan(&completed, &remaining))
			require.Positive(t, completed, "실패 전에 끝난 batch는 commit되어야 한다")
			require.Positive(t, remaining, "실패한 batch는 미완료 상태로 남아야 한다")
			require.Equal(t, total, completed+remaining)

			_, err = pool.Exec(t.Context(), "DROP TRIGGER compaction_last_row_failure ON "+table)
			require.NoError(t, err)
			require.NoError(t, applyMigrationFile(t.Context(), pool, dir, scenario.migration))
			require.NoError(t, pool.QueryRow(t.Context(), fmt.Sprintf("SELECT count(id) FILTER (WHERE %s IS NOT NULL),count(id) FILTER (WHERE %s IS NULL) FROM %s", column, column, table)).Scan(&completed, &remaining))
			require.Equal(t, total, completed)
			require.Zero(t, remaining)
		})
	}
}

func installCompactionLastRowFailure(t *testing.T, pool *pgxpool.Pool, table string, lastID int64) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `CREATE FUNCTION fail_compaction_last_row() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.id=TG_ARGV[0]::bigint THEN RAISE EXCEPTION 'injected final batch failure'; END IF; RETURN NEW; END $$`)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), fmt.Sprintf("CREATE TRIGGER compaction_last_row_failure BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION fail_compaction_last_row('%d')", table, lastID))
	require.NoError(t, err)
}
