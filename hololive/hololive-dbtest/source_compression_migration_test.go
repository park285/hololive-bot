package dbtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinaryHashesPreserveRoundTripAndIdentity(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	) INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,
		contract_generation,scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,
		evidence_sha256,provenance_id,fence_epoch,projection_generation)
		SELECT 'youtubejs','community_page','UC_binary',sha256('binary-slot'::bytea),1,1,now(),now(),decode(repeat('a',64),'hex'),
		'COMPLETE','CONTIGUOUS',id,decode(repeat('c',64),'hex'),source_observation_provenance_id('binary','migration','community_collect'),1,1 FROM payload;
		INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		SELECT id,source_observation_entity_kind_id('community_window'),'binary',source_observation_decision_id('APPLIED') FROM source_observations;
		INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
		VALUES (100,now(),decode(repeat('c',64),'hex'),now(),now(),decode(repeat('a',64),'hex'),'{"requested_channel_ids":["UC_binary"]}');
		INSERT INTO youtube_content_absence_slots(channel_id,observation_kind,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
		VALUES ('UC_binary','video_list',now(),decode(repeat('c',64),'hex'),now(),now(),decode(repeat('a',64),'hex'),'{}')`)
	require.NoError(t, err)

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
		'youtubejs','community_page','UC_binary',encode(sha256('binary-slot'::bytea),'hex'),1::smallint,1::bigint)`).Scan(&id, &digest))
	require.Equal(t, strings.Repeat("c", 64), digest)
	require.NoError(t, pool.QueryRow(ctx, `SELECT evidence_sha256 FROM lock_source_observation($1)`, id).Scan(&digest))
	require.Equal(t, strings.Repeat("c", 64), digest)

	var applications int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications
		WHERE observation_id=$1 AND entity_kind_id=source_observation_entity_kind_id('community_window') AND entity_key='binary' AND decision_id=source_observation_decision_id('APPLIED')`, id).Scan(&applications))
	require.Equal(t, 1, applications, "고아 정리는 원본이 남은 감사를 삭제하지 않는다")

	_, err = pool.Exec(ctx, `DELETE FROM source_observations WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_applications`).Scan(&applications))
	require.Zero(t, applications)
}
