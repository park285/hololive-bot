package sourceobservation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestRetentionProtectedWindowContinuesLaterStepsAndRestart(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := NewRepository(pool)
	ids := publishProcessedObservations(ctx, t, pool, repo, 2)
	ageObservations(t, pool, ids[:1], 72*time.Hour)
	ageObservations(t, pool, ids[1:], 48*time.Hour)

	_, err := pool.Exec(ctx, `WITH copies AS (
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
			scheduled_for,observed_at,received_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
			provenance_id, fence_epoch,projection_generation)
		SELECT provider,observation_kind,subject_key,sha256(('protected-copy-'||s)::bytea),schema_version,contract_generation,
			scheduled_for,observed_at,received_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
			provenance_id, fence_epoch,projection_generation
		FROM source_observations CROSS JOIN generate_series(1,999) s WHERE id=$1 RETURNING id
	) INSERT INTO source_observation_queue(observation_id) SELECT id FROM copies`, ids[0])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM source_observation_queue WHERE observation_id=$1`, ids[1])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status='RETIRED';
		UPDATE youtube_collection_job_leases SET slot_state='IDLE',owner_instance=NULL,lease_expires_at=NULL,retry_not_before=NULL;
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload,created_at)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('d',64),'hex'),'{}',now()-interval '2 hours');
		INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
		VALUES (91001,now()-interval '31 days',decode(repeat('a',64),'hex'),now()-interval '31 days',now()-interval '31 days',
			decode(repeat('b',64),'hex'),'{"requested_channel_ids":["UC_TEST"]}')`)
	require.NoError(t, err)
	insertRetentionCheckpoint(t, pool, "tick-progress", "a", 10*24*time.Hour)
	insertRetentionCheckpoint(t, pool, "tick-progress", "b", time.Hour)

	cfg := RetentionConfig{
		EvidenceAgeByKind:  map[contract.ObservationKind]time.Duration{contract.KindCommunityPage: 24 * time.Hour},
		LiveAbsenceSlotAge: 30 * 24 * time.Hour, CheckpointHistoryAge: 7 * 24 * time.Hour, BatchSize: 1000,
	}
	result, err := repo.RunRetentionTick(ctx, cfg, time.Now().UTC())
	require.NoError(t, err)

	part := evidenceRetentionPart(t, result)
	require.Zero(t, part.Deleted)
	require.False(t, part.BacklogKnown)

	for _, table := range []string{"source_observation_payloads", "youtube_live_absence_slots", "source_collection_checkpoints"} {
		var deleted int64

		for _, step := range result.ByTable {
			if step.Table == table {
				deleted = step.Deleted
			}
		}

		require.EqualValues(t, 1, deleted, "later cleanup %s must run after an all-protected window", table)
	}

	// 재생성한 Repository도 프로세스 메모리가 아니라 DB 커서 다음 창을 처리한다.
	result, err = NewRepository(pool).RunRetentionTick(ctx, cfg, time.Now().UTC())
	require.NoError(t, err)
	require.EqualValues(t, 1, evidenceRetentionPart(t, result).Deleted)
	assertTableCount(t, pool, "source_observations", 1000)
}
