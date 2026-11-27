package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type vocabularyRow struct {
	provider, kind, completeness, continuity         string
	providerID, kindID, completenessID, continuityID int16
}

func insertVocabularyObservation(t *testing.T, pool *pgxpool.Pool, subject, columns, values string) error {
	t.Helper()

	return insertInstanceVocabularyObservation(t, pool, subject, columns, values, "collector-a", 1)
}

func insertInstanceVocabularyObservation(
	t *testing.T, pool *pgxpool.Pool, subject, columns, values, instance string, generation int64,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',sha256($1::text::bytea),'{}')
		RETURNING id
	) INSERT INTO source_observations(`+columns+`,subject_key,observation_key,schema_version,contract_generation,
		scheduled_for,observed_at,scope_sha256,payload_id,evidence_sha256,provenance_id,fence_epoch,projection_generation)
	SELECT `+values+`,$1::text,sha256($1::text::bytea),1,1,now(),now(),decode(repeat('a',64),'hex'),payload.id,
		decode(repeat('c',64),'hex'),source_observation_provenance_id('job-vocab',$3::text,'community_collect'),1,$2
	FROM payload`, subject, generation, instance)

	return err
}

func storedVocabulary(t *testing.T, pool *pgxpool.Pool, subject string) vocabularyRow {
	t.Helper()

	var row vocabularyRow

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT provider,observation_kind,completeness,continuity,
		provider_id,observation_kind_id,completeness_id,continuity_id FROM source_observations WHERE subject_key=$1`, subject).
		Scan(&row.provider, &row.kind, &row.completeness, &row.continuity,
			&row.providerID, &row.kindID, &row.completenessID, &row.continuityID))

	return row
}

func TestSourceObservationVocabularyDerivesIDsFromNames(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	require.NoError(t, insertVocabularyObservation(t, pool, "named", "provider,observation_kind,completeness,continuity",
		"'youtubejs','community_page','PARTIAL','GAP_UNRESOLVED'"))

	require.Equal(t, vocabularyRow{
		provider: "youtubejs", kind: "community_page", completeness: "PARTIAL", continuity: "GAP_UNRESOLVED",
		providerID: 2, kindID: 1, completenessID: 2, continuityID: 2,
	}, storedVocabulary(t, pool, "named"))
}

func TestSourceObservationVocabularyDerivesNamesFromIDs(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	require.NoError(t, insertVocabularyObservation(t, pool, "identified",
		"provider_id,observation_kind_id,completeness_id,continuity_id", "2,1,1,1"))

	require.Equal(t, vocabularyRow{
		provider: "youtubejs", kind: "community_page", completeness: "COMPLETE", continuity: "CONTIGUOUS",
		providerID: 2, kindID: 1, completenessID: 1, continuityID: 1,
	}, storedVocabulary(t, pool, "identified"))
}

func TestSourceObservationVocabularyRejectsDisagreeingOrUnknownValues(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	requireSQLState(t, insertVocabularyObservation(t, pool, "disagree",
		"provider,provider_id,observation_kind,completeness,continuity",
		"'youtubejs',1,'community_page','COMPLETE','CONTIGUOUS'"), "23503")
	requireSQLState(t, insertVocabularyObservation(t, pool, "unknown-name",
		"provider,observation_kind,completeness,continuity",
		"'youtubejs','community_page','DONE','CONTIGUOUS'"), "23502")
	requireSQLState(t, insertVocabularyObservation(t, pool, "unknown-id",
		"provider_id,observation_kind_id,completeness_id,continuity_id", "2,1,1,9"), "23502")
	requireSQLState(t, insertVocabularyObservation(t, pool, "no-contract",
		"provider_id,observation_kind_id,completeness_id,continuity_id", "3,1,1,1"), "23503")
}

func TestSourceObservationVocabularyFenceAcceptsNameOrIDWritersUnderCollectorIdentity(t *testing.T) {
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
	fenced := func(subject, columns, values string) error {
		return insertInstanceVocabularyObservation(t, collector, subject, columns, values, "collector-c", 9201)
	}

	require.NoError(t, fenced("fenced-name",
		"provider,observation_kind,completeness,continuity", "'youtubejs','community_page','COMPLETE','CONTIGUOUS'"))
	require.NoError(t, fenced("fenced-id", "provider_id,observation_kind_id,completeness_id,continuity_id", "2,1,1,1"))
	requireSQLState(t, fenced("fenced-other-provider",
		"provider_id,observation_kind_id,completeness_id,continuity_id", "1,1,1,1"), "42501")

	require.Equal(t, int16(2), storedVocabulary(t, pool, "fenced-name").providerID)
	require.Equal(t, "youtubejs", storedVocabulary(t, pool, "fenced-id").provider)
}
