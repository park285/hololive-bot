package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const vocabularyIDColumns = "provider_id,observation_kind_id,completeness_id,continuity_id"

func insertVocabularyObservation(t *testing.T, pool *pgxpool.Pool, subject, values string) error {
	t.Helper()

	return insertInstanceVocabularyObservation(t, pool, subject, values, "collector-a", 1)
}

func insertInstanceVocabularyObservation(
	t *testing.T, pool *pgxpool.Pool, subject, values, instance string, generation int64,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',sha256($1::text::bytea),'{}')
		RETURNING id
	) INSERT INTO source_observations(`+vocabularyIDColumns+`,subject_key,observation_key,schema_version,contract_generation,
		scheduled_for,observed_at,scope_sha256,payload_id,evidence_sha256,provenance_id,fence_epoch,projection_generation)
	SELECT `+values+`,$1::text,sha256($1::text::bytea),1,1,now(),now(),decode(repeat('a',64),'hex'),payload.id,
		decode(repeat('c',64),'hex'),source_observation_provenance_id('job-vocab',$3::text,'community_collect'),1,$2
	FROM payload`, subject, generation, instance)

	return err
}

func storedVocabularyNames(t *testing.T, pool *pgxpool.Pool, subject string) [4]string {
	t.Helper()

	var names [4]string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT provider_value.name, kind.name, completeness_value.name, continuity_value.name
		FROM source_observations observation
		JOIN source_observation_providers provider_value ON provider_value.id = observation.provider_id
		JOIN source_observation_kinds kind ON kind.id = observation.observation_kind_id
		JOIN source_observation_completeness_values completeness_value ON completeness_value.id = observation.completeness_id
		JOIN source_observation_continuity_values continuity_value ON continuity_value.id = observation.continuity_id
		WHERE observation.subject_key = $1`, subject).Scan(&names[0], &names[1], &names[2], &names[3]))

	return names
}

func TestSourceObservationVocabularyStoresDictionaryIDs(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	require.NoError(t, insertVocabularyObservation(t, pool, "identified", "2,1,2,2"))

	require.Equal(t, [4]string{"youtubejs", "community_page", "PARTIAL", "GAP_UNRESOLVED"},
		storedVocabularyNames(t, pool, "identified"))
}

func TestSourceObservationVocabularyRejectsUnknownIDsAndUncontractedPairs(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	requireSQLState(t, insertVocabularyObservation(t, pool, "unknown-completeness", "2,1,9,1"), "23503")
	requireSQLState(t, insertVocabularyObservation(t, pool, "unknown-continuity", "2,1,1,9"), "23503")
	requireSQLState(t, insertVocabularyObservation(t, pool, "no-contract", "3,1,1,1"), "23503")
}

func TestSourceObservationVocabularyFenceMatchesLeaseProviderUnderCollectorIdentity(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,activated_at) OVERRIDING SYSTEM VALUE
	VALUES (9201,'RETIRED',0,repeat('c',64),now(),now());
	INSERT INTO youtube_collection_job_leases
		(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
		 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at,fence_epoch)
	VALUES ('job-vocab','youtubejs','SUBJECT','community_collect','fenced',9201,120000,
		'ACTIVE',now(),now(),'collector-c',now()+interval '1 minute',1);
	GRANT SELECT, INSERT ON source_observations, source_observation_payloads TO hololive_collector_c;
	GRANT SELECT ON source_observation_providers, source_observation_kinds,
		source_observation_completeness_values, source_observation_continuity_values TO hololive_collector_c;
	GRANT USAGE ON SEQUENCE source_observations_id_seq, source_observation_payloads_id_seq TO hololive_collector_c;
	GRANT EXECUTE ON FUNCTION source_observation_provenance_id(text,text,text) TO hololive_collector_c`)
	require.NoError(t, err)

	collector := eventGradeSession(t, pool, "hololive_collector_c")
	fenced := func(subject, values string) error {
		return insertInstanceVocabularyObservation(t, collector, subject, values, "collector-c", 9201)
	}

	require.NoError(t, fenced("fenced", "2,1,1,1"))
	requireSQLState(t, fenced("fenced-other-provider", "1,1,1,1"), "42501")
	require.Equal(t, "youtubejs", storedVocabularyNames(t, pool, "fenced")[0])
}
