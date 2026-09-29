package dbtest

import (
	jsonv2 "encoding/json/v2"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const channelStatisticsRemovalMigration = "234_remove_channel_statistics.sql"

func TestChannelStatisticsRemovalPreservesOtherKindsAndResumes(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	seedChannelStatisticsRemoval(t, pool)

	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration))
	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration))

	var removed, preserved, queued, applications int

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE observation_kind='channel_stats'),
		       count(*) FILTER (WHERE observation_kind='channel_profile' AND subject_key='profile-preserved')
		FROM source_observations`).Scan(&removed, &preserved))
	require.Zero(t, removed)
	require.Equal(t, 1, preserved)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_queue`).Scan(&queued))
	require.Equal(t, 1, queued)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications`).Scan(&applications))
	require.Equal(t, 1, applications)

	var profile, notification string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT handle FROM youtube_channel_profile_heads WHERE channel_id='profile-preserved'`).Scan(&profile))
	require.Equal(t, "@preserved", profile)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT kind FROM youtube_notification_outbox WHERE content_id='keep-video'`).Scan(&notification))
	require.Equal(t, "NEW_VIDEO", notification)

	var storedStatsTables int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname IN (
		'youtube_channel_stats_snapshots','youtube_channel_stats_evidence','youtube_channel_stats_heads',
		'youtube_stats_history','youtube_stats_changes','youtube_milestones','youtube_milestone_approaching'
	)`).Scan(&storedStatsTables))
	require.Zero(t, storedStatsTables)

	var targets int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_targets WHERE observation_kind='channel_profile'`).Scan(&targets))
	require.Equal(t, 1, targets)

	var activeProjection bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT valid_until>now() FROM youtube_collection_projection_generations WHERE status='CURRENT'`).Scan(&activeProjection))
	require.False(t, activeProjection, "changed target set must not retain the old valid projection hash")
}

func TestChannelStatisticsRemovalRejectsProcessingObservationBeforeDeleting(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	seedChannelStatisticsRemoval(t, pool)

	_, err := pool.Exec(t.Context(), `UPDATE source_observation_queue SET status='PROCESSING',lease_owner='active-consumer',
		lease_token=repeat('a',64),lease_expires_at=now()+interval '1 minute'
		WHERE observation_id=(SELECT min(id) FROM source_observations WHERE observation_kind='channel_stats')`)
	require.NoError(t, err)

	err = applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration)
	require.ErrorContains(t, err, "requires drained metadata publishers and statistics consumers")

	var stats int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observations WHERE observation_kind='channel_stats'`).Scan(&stats))
	require.Equal(t, 1501, stats)
}

func TestChannelStatisticsRemovalPreservesUnresolvedDeliveryForReview(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	seedChannelStatisticsRemoval(t, pool)

	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_notification_delivery_ledger
		(kind,logical_id,room_id,status,first_recorded_at,updated_at,quarantined_at)
		VALUES ('MILESTONE','unknown-statistics-delivery','review-room','QUARANTINED',now(),now(),now())`)
	require.NoError(t, err)

	err = applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration)
	require.ErrorContains(t, err, "review of unresolved milestone deliveries")

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status FROM youtube_notification_delivery_ledger WHERE logical_id='unknown-statistics-delivery'`).Scan(&status))
	require.Equal(t, "QUARANTINED", status)
}

func TestChannelStatisticsRemovalPreservesDurableNotificationHistory(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	seedChannelStatisticsRemoval(t, pool)

	_, err := pool.Exec(t.Context(), `INSERT INTO alarm_dispatch_events
		(event_key,payload_hash,alarm_type,payload_schema_version,payload)
		VALUES ('review-statistics',repeat('a',64),'LIVE',3,'{"source_kind":"youtube_outbox","youtube_outbox":{"kind":"MILESTONE"}}')`)
	require.NoError(t, err)

	err = applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration)
	require.ErrorContains(t, err, "review of durable milestone dispatch history")

	var events int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM alarm_dispatch_events WHERE event_key='review-statistics'`).Scan(&events))
	require.Equal(t, 1, events)
}

func TestChannelStatisticsRemovalBoundsContractReferenceLookup(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO source_observation_applications
			(provider,observation_kind,subject_key,evidence_sha256,entity_kind,entity_key,decision,effective_at)
		SELECT provider,observation_kind,'retained-'||n,repeat('a',64),'retained','retained-'||n,'APPLIED',now()
		FROM observation_contract_generations CROSS JOIN generate_series(1,1000) n
		WHERE observation_kind <> 'channel_stats'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, channelStatisticsRemovalMigration))

	_, err = pool.Exec(t.Context(), `ANALYZE source_observation_applications`)
	require.NoError(t, err)

	// 등록행 삭제의 FK 확인과 같은 조건이다. 다른 kind의 감사가 많아도 전체를 읽지 않아야 한다.
	var raw []byte

	require.NoError(t, pool.QueryRow(t.Context(), `EXPLAIN (ANALYZE, FORMAT JSON)
		SELECT 1 FROM ONLY source_observation_applications AS application
		WHERE provider='youtubejs' AND observation_kind='channel_stats'
		FOR KEY SHARE OF application`).Scan(&raw))

	type lookupPlan struct {
		Relation string       `json:"Relation Name"`
		Rows     float64      `json:"Actual Rows"`
		Removed  float64      `json:"Rows Removed by Filter"`
		Loops    float64      `json:"Actual Loops"`
		Plans    []lookupPlan `json:"Plans"`
	}

	var plans []struct{ Plan lookupPlan }

	require.NoError(t, jsonv2.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)

	nodes := []lookupPlan{plans[0].Plan}
	visits := float64(0)

	for len(nodes) > 0 {
		node := nodes[len(nodes)-1]

		nodes = nodes[:len(nodes)-1]

		if node.Relation == "source_observation_applications" {
			visits += (node.Rows + node.Removed) * node.Loops
		}

		nodes = append(nodes, node.Plans...)
	}

	require.LessOrEqual(t, visits, float64(128), "contract removal must not scan unrelated retained applications")
}

func channelStatisticsRemovalPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(t.Context(), pool))

	for _, filename := range entries {
		if filename == channelStatisticsRemovalMigration {
			return pool, dir
		}

		require.NoError(t, applyManifestMigration(t.Context(), pool, dir, filename))
	}

	t.Fatal("channel statistics removal migration is not registered")

	return nil, ""
}

func seedChannelStatisticsRemoval(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// 234 이전의 실제 schema로 여러 배치의 원본/자식과 보존할 다른 kind를 함께 만듭니다.
	_, err := pool.Exec(t.Context(), `
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,
		contract_generation,scheduled_for,observed_at,scope_sha256,completeness,continuity,payload,payload_sha256,
		evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
		SELECT 'youtubejs','channel_stats','stats-'||n,'stats-'||n,1,1,now(),now(),repeat('a',64),
		'COMPLETE','NOT_APPLICABLE','{}',repeat('b',64),repeat('c',64),'collector','metadata','youtubejs_channel_metadata',1,1
		FROM generate_series(1,1501) n;
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,
		contract_generation,scheduled_for,observed_at,scope_sha256,completeness,continuity,payload,payload_sha256,
		evidence_sha256,collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
		VALUES ('youtubejs','channel_profile','profile-preserved','profile-preserved',1,1,now(),now(),repeat('a',64),
		'COMPLETE','NOT_APPLICABLE','{}',repeat('b',64),repeat('c',64),'collector','metadata','youtubejs_channel_metadata',1,1);
		INSERT INTO source_observation_queue(observation_id) SELECT id FROM source_observations;
		INSERT INTO source_observation_applications(observation_id,provider,observation_kind,subject_key,evidence_sha256,entity_kind,entity_key,decision,effective_at)
		SELECT id,provider,observation_kind,subject_key,evidence_sha256,observation_kind,subject_key,'APPLIED',now() FROM source_observations;
		INSERT INTO youtube_channel_profile_heads(channel_id,handle_set,handle,handle_effective_at)
		VALUES ('profile-preserved',true,'@preserved',now());
		INSERT INTO youtube_channel_stats_snapshots(channel_id,captured_at,subscriber_count) VALUES ('stats-original',now(),100);
		INSERT INTO youtube_stats_changes(channel_id,subscriber_change) VALUES ('stats-original',10);
		INSERT INTO youtube_stats_history(time,channel_id,subscribers) VALUES (now(),'stats-original',100);
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',2,repeat('d',64),now()+interval '1 hour',now());
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
		SELECT generation,'shared-channel',kind,50,60000,true,now()+interval '1 hour'
		FROM youtube_collection_projection_generations CROSS JOIN (VALUES ('channel_stats'),('channel_profile')) kinds(kind);
		INSERT INTO youtube_notification_outbox(kind,channel_id,content_id,payload)
		VALUES ('NEW_VIDEO','profile-preserved','keep-video','{}'),('MILESTONE','stats-original','remove-statistics','{}');
	`)
	require.NoError(t, err)
}
