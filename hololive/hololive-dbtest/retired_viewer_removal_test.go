package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const retiredViewerRemovalMigration = "306_remove_retired_viewer_storage.sql"

func TestRetiredViewerRemovalPreservesActiveCollectionAndReplays(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, retiredViewerRemovalMigration)
	seedRetiredViewerProducts(t, pool)
	seedViewerRemovalObservation(t, pool, "live_snapshot")

	_, err := pool.Exec(t.Context(), `
		INSERT INTO source_observation_queue(observation_id,status,lease_owner,lease_token,lease_expires_at)
		SELECT id,'PROCESSING','consumer',repeat('a',64),now()+interval '1 hour' FROM source_observations;
		INSERT INTO source_observation_replay_requests
			(observation_id,provider,observation_kind,subject_key,observation_key,evidence_sha256,requested_by,reason,previous_attempt_count)
		SELECT id,provider,observation_kind,subject_key,observation_key,evidence_sha256,'operator','retain',0 FROM source_observations;
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',1,repeat('a',64),now()+interval '1 hour',now()),
		       ('STAGING',1,repeat('b',64),now()+interval '1 hour',NULL);
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
		SELECT generation,'UC_KEEP','live_snapshot',50,60000,true FROM youtube_collection_projection_generations;
		INSERT INTO youtube_collection_job_leases
			(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
			 slot_state,scheduled_for,next_due_at,fence_epoch,owner_instance,lease_expires_at,membership_kinds,membership_target_count)
		SELECT 'live-lease','holodex','GLOBAL','holodex_live','global:holodex_live',generation,60000,
		       'ACTIVE',now(),now(),7,'collector',now()+interval '1 hour',ARRAY['live_snapshot'],1
		FROM youtube_collection_projection_generations WHERE status='CURRENT';`)
	require.NoError(t, err)
	seedViewerRemovalRuntimeState(t, pool)

	for _, attempt := range []string{"first", "reapply"} {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, retiredViewerRemovalMigration), attempt)

		var tables, registrations, observations, processing, replay, targets, projections, leases int

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname IN (
				'youtube_live_viewer_samples','youtube_live_viewer_sample_heads','youtube_live_viewer_sample_evidence',
				'youtube_stream_stats','youtube_channel_latest_stats')),
			(SELECT count(*) FROM observation_contract_generations WHERE observation_kind='viewer_sample'),
			(SELECT count(*) FROM source_observations WHERE observation_kind='live_snapshot'),
			(SELECT count(*) FROM source_observation_queue WHERE status='PROCESSING' AND lease_owner='consumer'
			 AND lease_token=repeat('a',64) AND lease_expires_at>now()),
			(SELECT count(*) FROM source_observation_replay_requests WHERE status='PENDING'),
			(SELECT count(*) FROM youtube_collection_targets WHERE observation_kind='live_snapshot'),
			(SELECT count(*) FROM youtube_collection_projection_generations WHERE status IN ('CURRENT','STAGING') AND valid_until>now()),
			(SELECT count(*) FROM youtube_collection_job_leases WHERE job_key='live-lease' AND slot_state='ACTIVE'
			 AND fence_epoch=7 AND owner_instance='collector' AND membership_target_count=1)
		`).Scan(&tables, &registrations, &observations, &processing, &replay, &targets, &projections, &leases))
		require.Zero(t, tables, attempt)
		require.Zero(t, registrations, attempt)
		require.Equal(t, 1, observations, attempt)
		require.Equal(t, 1, processing, attempt)
		require.Equal(t, 1, replay, attempt)
		require.Equal(t, 2, targets, attempt)
		require.Equal(t, 2, projections, attempt)
		require.Equal(t, 1, leases, attempt)

		var retiredState, retainedState int

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM source_collection_checkpoints WHERE observation_kind='viewer_sample') +
			(SELECT count(*) FROM source_observation_consumer_offsets WHERE observation_kind='viewer_sample') +
			(SELECT count(*) FROM youtube_collection_job_leases WHERE collection_job_kind='youtubejs_viewer'),
			(SELECT count(*) FROM source_collection_checkpoints WHERE observation_kind='live_snapshot' AND last_observation_key='old-key') +
			(SELECT count(*) FROM source_observation_consumer_offsets WHERE observation_kind='live_snapshot' AND last_processed_id=456)
		`).Scan(&retiredState, &retainedState))
		require.Zero(t, retiredState, attempt)
		require.Equal(t, 2, retainedState, attempt)
	}

	for _, query := range []string{
		`INSERT INTO observation_contract_generations(provider,observation_kind,current_schema_version,current_generation,updated_by)
		 VALUES ('youtubejs','viewer_sample',1,1,'test')`,
		`INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
		 SELECT generation,'retired','viewer_sample',50,60000,true FROM youtube_collection_projection_generations WHERE status='CURRENT'`,
		`INSERT INTO source_observation_consumer_offsets(consumer_name,observation_kind) VALUES ('consumer','viewer_sample')`,
	} {
		_, err := pool.Exec(t.Context(), query)
		requireSQLState(t, err, "23514")
	}
}

func TestRetiredViewerRemovalRejectsUnapprovedHistoryAtomically(t *testing.T) {
	for name, seed := range map[string]string{
		"payload": `INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
			VALUES ('viewer_sample',1,'source-observation-canonical-json-v1',decode(repeat('a',64),'hex'),'{}')`,
		"orphan replay": `INSERT INTO source_observation_replay_requests
			(provider,observation_kind,subject_key,observation_key,evidence_sha256,requested_by,reason,previous_attempt_count)
			VALUES ('youtubejs','viewer_sample','video','key',repeat('a',64),'operator','retain',0)`,
		"orphan audit": `INSERT INTO source_observation_applications
			(provider,observation_kind,subject_key,evidence_sha256,entity_kind,entity_key,decision,effective_at)
			VALUES ('youtubejs','viewer_sample','video',repeat('a',64),'youtube_live_viewer_sample','video','APPLIED',now())`,
		"retired projection": `INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
			VALUES ('RETIRED',1,repeat('a',64),now()-interval '1 day',now()-interval '2 days');
			INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
			SELECT generation,'video','viewer_sample',50,60000,true FROM youtube_collection_projection_generations`,
		"active legacy membership": `INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
			VALUES ('CURRENT',0,repeat('a',64),now()+interval '1 hour',now());
			INSERT INTO youtube_collection_job_leases
				(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
				 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at,membership_kinds)
			SELECT 'legacy','holodex','GLOBAL','holodex_live','global:holodex_live',generation,60000,
			       'ACTIVE',now(),now(),'collector',now()+interval '1 hour',ARRAY['live_snapshot','viewer_sample']
			FROM youtube_collection_projection_generations`,
		"active dedicated lease": `INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
			VALUES ('CURRENT',0,repeat('a',64),now()+interval '1 hour',now());
			INSERT INTO youtube_collection_job_leases
				(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
				 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at,membership_kinds)
			SELECT 'legacy','youtubejs','SUBJECT','youtubejs_viewer','video',generation,60000,
			       'ACTIVE',now(),now(),'collector',now()+interval '1 hour',ARRAY['viewer_sample']
			FROM youtube_collection_projection_generations`,
		"unexpected dependency": `CREATE TABLE viewer_dependency(video_id text REFERENCES youtube_live_viewer_sample_heads(video_id));
			INSERT INTO viewer_dependency VALUES ('retired')`,
	} {
		t.Run(name, func(t *testing.T) {
			pool, dir := projectionMigrationPoolBefore(t, retiredViewerRemovalMigration)
			seedRetiredViewerProducts(t, pool)

			_, err := pool.Exec(t.Context(), seed)
			require.NoError(t, err)
			require.Error(t, applyMigrationFile(t.Context(), pool, dir, retiredViewerRemovalMigration))
			assertRetiredViewerProductsKept(t, pool)
		})
	}
}

func TestRetiredViewerRemovalKeepsPendingObservation(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, retiredViewerRemovalMigration)
	seedRetiredViewerProducts(t, pool)
	seedViewerRemovalObservation(t, pool, "viewer_sample")

	_, err := pool.Exec(t.Context(), `INSERT INTO source_observation_queue(observation_id) SELECT id FROM source_observations`)
	require.NoError(t, err)
	require.ErrorContains(t, applyMigrationFile(t.Context(), pool, dir, retiredViewerRemovalMigration), "retained history in public.source_observations")
	assertRetiredViewerProductsKept(t, pool)

	var pending int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_queue WHERE status='PENDING'`).Scan(&pending))
	require.Equal(t, 1, pending)
}

func seedRetiredViewerProducts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_viewer_samples(video_id,captured_at,channel_id,concurrent_viewers) VALUES ('retired',now(),'UC_RETIRED',123);
		INSERT INTO youtube_live_viewer_sample_heads(video_id) VALUES ('retired');
		INSERT INTO youtube_live_viewer_sample_evidence(video_id,sample_window_start,provider,viewer_count,availability,
			sample_window_seconds,scheduled_for,effective_at,received_at)
		VALUES ('retired',now(),'youtubejs',123,'AVAILABLE',60,now(),now(),now());
		INSERT INTO youtube_stream_stats(video_id,channel_id,sample_count) VALUES ('retired','UC_RETIRED',1);
		INSERT INTO youtube_channel_latest_stats(channel_id,time,subscribers) VALUES ('UC_RETIRED',now(),123);`)
	require.NoError(t, err)
}

func seedViewerRemovalObservation(t *testing.T, pool *pgxpool.Pool, kind string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		WITH payload AS (
			INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
			VALUES ($1,1,'source-observation-canonical-json-v1',decode(repeat('a',64),'hex'),'{}') RETURNING id
		)
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
			scheduled_for,observed_at,scope_sha256,completeness,continuity,evidence_sha256,collector_instance,job_key,
			collection_job_kind,fence_epoch,projection_generation,payload_id)
		SELECT 'youtubejs',$1,'UC_KEEP','keep-observation',1,1,now(),now(),repeat('b',64),'PARTIAL','NOT_APPLICABLE',
		       repeat('c',64),'collector','keep-job','youtubejs_channel_live',1,1,id FROM payload`, kind)
	require.NoError(t, err)
}

func assertRetiredViewerProductsKept(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var samples, heads, evidence, streams, channels, contracts int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM youtube_live_viewer_samples),
		(SELECT count(*) FROM youtube_live_viewer_sample_heads),
		(SELECT count(*) FROM youtube_live_viewer_sample_evidence),
		(SELECT count(*) FROM youtube_stream_stats),
		(SELECT count(*) FROM youtube_channel_latest_stats),
		(SELECT count(*) FROM observation_contract_generations WHERE observation_kind='viewer_sample')
	`).Scan(&samples, &heads, &evidence, &streams, &channels, &contracts))
	require.Equal(t, 1, samples)
	require.Equal(t, 1, heads)
	require.Equal(t, 1, evidence)
	require.Equal(t, 1, streams)
	require.Equal(t, 1, channels)
	require.Equal(t, 2, contracts)
}

func seedViewerRemovalRuntimeState(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO source_collection_checkpoints(provider,observation_kind,subject_key,scope_sha256,contract_generation,
			last_observation_key,last_evidence_sha256,last_scheduled_for,last_success_at,collection_latency_ms,continuity)
		SELECT provider,kind,'video',repeat('a',64),1,'old-key',repeat('b',64),now(),now(),1,'NOT_APPLICABLE'
		FROM (VALUES ('youtubejs','viewer_sample'),('holodex','viewer_sample'),('youtubejs','live_snapshot')) AS fixture(provider,kind);
		INSERT INTO source_observation_consumer_offsets(consumer_name,observation_kind,last_processed_id)
		VALUES ('consumer','viewer_sample',123),('consumer','live_snapshot',456);
		INSERT INTO youtube_collection_job_leases
			(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
			 slot_state,scheduled_for,next_due_at,membership_kinds)
		SELECT 'retired-lease','youtubejs','SUBJECT','youtubejs_viewer','video',generation,60000,
		       'IDLE',now(),now(),ARRAY['viewer_sample']
		FROM youtube_collection_projection_generations WHERE status='CURRENT';`)
	require.NoError(t, err)
}
