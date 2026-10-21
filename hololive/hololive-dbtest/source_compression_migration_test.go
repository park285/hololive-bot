package dbtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	applicationCascadeMigration = "307_source_application_cascade.sql"
	binaryHashesMigration       = "308_source_binary_hashes.sql"
)

func TestApplicationCascadeMigrationResumesCommittedOrphanBatches(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, applicationCascadeMigration)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `INSERT INTO source_observation_applications
		(provider,observation_kind,subject_key,evidence_sha256,entity_kind,entity_key,decision,effective_at)
		SELECT 'youtubejs','community_page','UC_migration',repeat('a',64),'community_window','orphan-'||n,'APPLIED',now()
		FROM generate_series(1,2501) n;
		CREATE FUNCTION interrupt_orphan_purge() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF OLD.entity_key='orphan-1501' THEN RAISE EXCEPTION 'interrupt orphan purge'; END IF; RETURN OLD; END $$;
		CREATE TRIGGER interrupt_orphan_purge BEFORE DELETE ON source_observation_applications
		FOR EACH ROW EXECUTE FUNCTION interrupt_orphan_purge()`)
	require.NoError(t, err)
	require.ErrorContains(t, applyMigrationFile(ctx, pool, dir, applicationCascadeMigration), "interrupt orphan purge")

	var remaining int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications`).Scan(&remaining))
	require.Equal(t, 1501, remaining, "첫 1000행의 실제 COMMIT은 뒤 배치 실패에도 남는다")

	_, err = pool.Exec(ctx, `DROP TRIGGER interrupt_orphan_purge ON source_observation_applications;
		DROP FUNCTION interrupt_orphan_purge()`)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, applicationCascadeMigration))
	}

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications`).Scan(&remaining))
	require.Zero(t, remaining)

	_, err = pool.Exec(ctx, `INSERT INTO source_observation_applications(entity_kind,entity_key,decision)
		VALUES ('community_window','orphan','APPLIED')`)
	require.Error(t, err)
}

func TestBinaryHashesMigrationPreservesRoundTripAndIdentity(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, applicationCascadeMigration)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	) INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,
		contract_generation,scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,
		evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
		SELECT 'youtubejs','community_page','UC_binary','binary-slot',1,1,now(),now(),repeat('a',64),
		'COMPLETE','CONTIGUOUS',id,repeat('c',64),'migration','binary','community_collect',1,1 FROM payload;
		INSERT INTO source_observation_applications(observation_id,provider,observation_kind,subject_key,evidence_sha256,
		entity_kind,entity_key,decision,effective_at)
		SELECT id,provider,observation_kind,subject_key,evidence_sha256,'community_window','binary','APPLIED',scheduled_for FROM source_observations;
		INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
		VALUES (100,now(),repeat('c',64),now(),now(),repeat('a',64),'{"requested_channel_ids":["UC_binary"]}');
		INSERT INTO youtube_content_absence_slots(channel_id,observation_kind,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
		VALUES ('UC_binary','video_list',now(),repeat('c',64),now(),now(),repeat('a',64),'{}')`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, applicationCascadeMigration))

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, binaryHashesMigration))
	}

	for _, table := range []string{sourceObservationsTable, "youtube_live_absence_slots", "youtube_content_absence_slots"} {
		var (
			scope, evidence             string
			scopeLength, evidenceLength int
		)

		require.NoError(t, pool.QueryRow(ctx, `SELECT encode(scope_sha256,'hex'),encode(evidence_sha256,'hex'),
			octet_length(scope_sha256),octet_length(evidence_sha256) FROM `+table).Scan(&scope, &evidence, &scopeLength, &evidenceLength))
		require.Equal(t, strings.Repeat("a", 64), scope)
		require.Equal(t, strings.Repeat("c", 64), evidence)
		require.Equal(t, 32, scopeLength)
		require.Equal(t, 32, evidenceLength)

		_, err = pool.Exec(ctx, `UPDATE `+table+` SET evidence_sha256=decode('aa','hex')`)
		require.Error(t, err, "32바이트가 아닌 해시는 거부한다")
	}

	var (
		id     int64
		digest string
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT id,evidence_sha256 FROM lock_source_observation_identity(
		'youtubejs','community_page','UC_binary','binary-slot',1::smallint,1::bigint)`).Scan(&id, &digest))
	require.Equal(t, strings.Repeat("c", 64), digest)
	require.NoError(t, pool.QueryRow(ctx, `SELECT evidence_sha256 FROM lock_source_observation($1)`, id).Scan(&digest))
	require.Equal(t, strings.Repeat("c", 64), digest)

	var applications int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications
		WHERE observation_id=$1 AND entity_kind='community_window' AND entity_key='binary' AND decision='APPLIED'`, id).Scan(&applications))
	require.Equal(t, 1, applications, "고아 정리는 원본이 남은 감사를 삭제하지 않는다")

	_, err = pool.Exec(ctx, `DELETE FROM source_observations WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications`).Scan(&applications))
	require.Zero(t, applications)
}
