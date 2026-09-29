package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadMigrationResumesBoundedBackfillAndReplaysCutover(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	ctx := t.Context()

	for _, file := range []string{
		"234_remove_channel_statistics.sql", "235_source_observation_application_active_unique.sql",
		"236_source_observation_application_drop_full_unique.sql", "237_bounded_projection_retention_and_vacuum.sql",
		"238_source_observation_payload_prepare.sql",
		"239_source_observation_payload_index.sql", "240_source_observation_payload_backfill_index.sql",
	} {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, file))
	}

	_, err := pool.Exec(ctx, `INSERT INTO source_observations
  (provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
   scheduled_for,observed_at,scope_sha256,completeness,continuity,payload,payload_sha256,
   evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
  SELECT 'youtubejs','community_page','migration-'||n,'migration-'||n,1,1,now(),now(),repeat('a',64),
   'COMPLETE','CONTIGUOUS','{}',encode(sha256('{}'::bytea),'hex'),repeat('c',64),
   'migration-test','migration-job','community_collect',1,1 FROM generate_series(1,2501) n`)
	require.NoError(t, err)
	require.ErrorContains(t, applyMigrationFile(ctx, pool, dir, "241_source_observation_payload_cutover.sql"), "backfill incomplete")

	var assigned int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observations WHERE payload_id IS NOT NULL`).Scan(&assigned))
	require.Zero(t, assigned, "failed cutover must roll back its final batch")

	for _, want := range []int{1000, 1000, 501, 0} {
		var migrated int

		require.NoError(t, pool.QueryRow(ctx, `SELECT backfill_source_observation_payloads(1000)`).Scan(&migrated))
		require.Equal(t, want, migrated)
	}

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, "241_source_observation_payload_cutover.sql"))
		require.NoError(t, applyMigrationFile(ctx, pool, dir, "242_drop_payload_backfill_index.sql"))
	}

	var observations, payloads int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT payload_id) FROM source_observations`).Scan(&observations, &payloads))
	require.Equal(t, 2501, observations)
	require.Equal(t, 1, payloads)

	var document, digest string

	require.NoError(t, pool.QueryRow(ctx, `SELECT payload::text,encode(payload_sha256,'hex') FROM source_observation_payloads`).Scan(&document, &digest))
	require.Equal(t, "{}", document)
	require.Equal(t, "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", digest)
}
