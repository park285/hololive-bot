package dbtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	provenanceCompactionMigration = "317_source_observation_provenance_compaction.sql"
	applicationVocabMigration     = "318_source_application_vocab_codes.sql"
)

var applicationVocabCutoverMigrations = []string{
	applicationVocabMigration,
	"319_source_application_entity_index.sql",
	"320_source_application_active_index_drop.sql",
	"321_source_application_vocab_cutover.sql",
}

// seedProvenanceCutoverRows는 317 이전 schema에 출처 문자열·hex 키를 가진 원본 3건과 보호 주변 행을 넣는다.
func seedProvenanceCutoverRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	) INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
		scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
		collector_instance,job_key,collection_job_kind,fence_epoch,projection_generation)
	SELECT 'youtubejs','community_page',row.subject,encode(sha256(row.subject::bytea),'hex'),1,1,now(),now(),
		decode(repeat('a',64),'hex'),'COMPLETE','CONTIGUOUS',payload.id,decode(repeat('c',64),'hex'),
		row.instance,row.job,'community_collect',1,1
	FROM payload CROSS JOIN (VALUES
		('leased','collector-a','job-leased'),
		('live','collector-a','job-live'),
		('shared','collector-a','job-leased')
	) AS row(subject,instance,job);
	INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,activated_at) OVERRIDING SYSTEM VALUE
	VALUES (9103,'RETIRED',0,repeat('c',64),now(),now());
	UPDATE source_observations SET projection_generation=9103;
	INSERT INTO youtube_collection_job_leases
		(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
		 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at)
	VALUES ('job-leased','youtubejs','SUBJECT','community_collect','leased',9103,120000,
		'ACTIVE',now(),now(),'collector-a',now()+interval '1 minute');
	INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('vocab-live','UC_vocab','LIVE');
	INSERT INTO source_observation_applications(observation_id,entity_kind,entity_key,decision)
	SELECT id,'youtube_live_session','vocab-live','APPLIED' FROM source_observations WHERE subject_key='live';
	INSERT INTO source_observation_applications(observation_id,entity_kind,entity_key,decision)
	SELECT id,'community_window','UC_vocab','CANONICALIZED' FROM source_observations WHERE subject_key='shared'`)
	require.NoError(t, err)
}

func observationIDBySubject(t *testing.T, pool *pgxpool.Pool, subject string) int64 {
	t.Helper()

	var id int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT id FROM source_observations WHERE subject_key=$1`, subject).Scan(&id))

	return id
}

func retentionProtected(t *testing.T, pool *pgxpool.Pool, id int64) bool {
	t.Helper()

	var protected bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT source_observation_retention_protected($1)`, id).Scan(&protected))

	return protected
}

func applyMigrationsTwice(ctx context.Context, t *testing.T, pool *pgxpool.Pool, dir string, files ...string) {
	t.Helper()

	for _, filename := range files {
		for range 2 {
			require.NoError(t, applyMigrationFile(ctx, pool, dir, filename), filename)
		}
	}
}

func TestProvenanceCompactionMigrationPreservesIdentityAndProtection(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, provenanceCompactionMigration)
	ctx := t.Context()
	seedProvenanceCutoverRows(t, pool)
	applyMigrationsTwice(ctx, t, pool, dir, provenanceCompactionMigration)

	var provenances, mismatched int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM source_observation_provenances`).Scan(&provenances))
	require.Equal(t, 2, provenances, "같은 출처 3-tuple은 사전 행 하나를 공유한다")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(o.id) FROM source_observations o
		JOIN source_observation_provenances p ON p.id=o.provenance_id
		WHERE p.collector_instance<>'collector-a' OR p.collection_job_kind<>'community_collect'
		   OR p.job_key<>CASE o.subject_key WHEN 'live' THEN 'job-live' ELSE 'job-leased' END
		   OR octet_length(o.observation_key)<>32 OR o.observation_key<>sha256(o.subject_key::bytea)`).Scan(&mismatched))
	require.Zero(t, mismatched, "출처 문자열과 hex 키는 손실 없이 복원된다")

	var column int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(attname) FROM pg_attribute
		WHERE attrelid='source_observations'::regclass AND NOT attisdropped
		  AND attname IN ('collector_instance','job_key','collection_job_kind')`).Scan(&column))
	require.Zero(t, column)

	leased := observationIDBySubject(t, pool, "leased")
	leasedKey := hexSHA256("leased")

	var (
		lockedID int64
		digest   string
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT id,evidence_sha256 FROM lock_source_observation_identity(
		'youtubejs','community_page','leased',$1,1::smallint,1::bigint)`, leasedKey).Scan(&lockedID, &digest))
	require.Equal(t, leased, lockedID)
	require.Equal(t, strings.Repeat("c", 64), digest)

	var lockedKey string

	require.NoError(t, pool.QueryRow(ctx, `SELECT observation_key FROM lock_source_observation($1)`, leased).Scan(&lockedKey))
	require.Equal(t, leasedKey, lockedKey, "잠금 API는 계속 hex text를 돌려준다")

	require.True(t, retentionProtected(t, pool, leased), "ACTIVE lease의 job_key 보호는 사전 표를 통해 유지된다")
	require.True(t, retentionProtected(t, pool, observationIDBySubject(t, pool, "shared")), "같은 사전 행을 공유하는 원본도 보호된다")
	require.True(t, retentionProtected(t, pool, observationIDBySubject(t, pool, "live")), "LIVE session 감사 보호는 유지된다")

	_, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET slot_state='IDLE',owner_instance=NULL,lease_expires_at=NULL`)
	require.NoError(t, err)
	require.False(t, retentionProtected(t, pool, leased))

	_, err = pool.Exec(ctx, `UPDATE source_observations SET observation_key=decode('aa','hex') WHERE id=$1`, leased)
	require.Error(t, err, "32바이트가 아닌 observation_key는 거부한다")

	var resolved, again int

	require.NoError(t, pool.QueryRow(ctx, `SELECT source_observation_provenance_id('job-new','collector-b','community_collect'),
		source_observation_provenance_id('job-new','collector-b','community_collect')`).Scan(&resolved, &again))
	require.Equal(t, resolved, again, "같은 3-tuple은 같은 사전 id로 resolve된다")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM source_observation_provenances`).Scan(&provenances))
	require.Equal(t, 3, provenances)

	_, err = pool.Exec(ctx, `SELECT source_observation_provenance_id('', 'collector-b', 'community_collect')`)
	require.Error(t, err, "빈 job_key는 사전 bounds가 거부한다")
}

func TestProvenanceCompactionMigrationRejectsNonHexObservationKey(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, provenanceCompactionMigration)
	ctx := t.Context()
	seedProvenanceCutoverRows(t, pool)

	_, err := pool.Exec(ctx, `UPDATE source_observations SET observation_key='not-a-digest' WHERE subject_key='shared'`)
	require.NoError(t, err)
	require.Error(t, applyMigrationFile(ctx, pool, dir, provenanceCompactionMigration))

	var textKeys int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(attname) FROM pg_attribute
		WHERE attrelid='source_observations'::regclass AND attname='observation_key' AND atttypid='text'::regtype`).Scan(&textKeys))
	require.Equal(t, 1, textKeys, "변환 실패는 bytea 전환 블록을 롤백한다")

	_, err = pool.Exec(ctx, `UPDATE source_observations SET observation_key=encode(sha256('shared'::bytea),'hex') WHERE subject_key='shared'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, provenanceCompactionMigration), "수정 뒤 재실행은 남은 블록만 적용한다")
	require.True(t, retentionProtected(t, pool, observationIDBySubject(t, pool, "leased")))
}

func TestApplicationVocabMigrationsPreserveAuditSemantics(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, provenanceCompactionMigration)
	ctx := t.Context()
	seedProvenanceCutoverRows(t, pool)
	applyMigrationsTwice(ctx, t, pool, dir, provenanceCompactionMigration)
	applyMigrationsTwice(ctx, t, pool, dir, applicationVocabCutoverMigrations...)

	var mismatched, kinds, decisions int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(a.id) FROM source_observation_applications a
		JOIN source_observation_entity_kinds k ON k.id=a.entity_kind_id
		JOIN source_observation_decisions d ON d.id=a.decision_id
		JOIN source_observations o ON o.id=a.observation_id
		WHERE (o.subject_key='live' AND (k.name<>'youtube_live_session' OR d.name<>'APPLIED'))
		   OR (o.subject_key='shared' AND (k.name<>'community_window' OR d.name<>'CANONICALIZED'))`).Scan(&mismatched))
	require.Zero(t, mismatched, "감사 어휘는 코드 표를 통해 손실 없이 복원된다")
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(id) FROM source_observation_entity_kinds),
		(SELECT count(id) FROM source_observation_decisions)`).Scan(&kinds, &decisions))
	require.Equal(t, 2, kinds)
	require.Equal(t, 2, decisions)

	var column int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(attname) FROM pg_attribute
		WHERE attrelid='source_observation_applications'::regclass AND NOT attisdropped AND attname IN ('entity_kind','decision')`).Scan(&column))
	require.Zero(t, column)

	var indexes []string

	rows, err := pool.Query(ctx, `SELECT indexname FROM pg_indexes WHERE tablename='source_observation_applications' ORDER BY 1`)
	require.NoError(t, err)

	for rows.Next() {
		var name string

		require.NoError(t, rows.Scan(&name))

		indexes = append(indexes, name)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, []string{"source_observation_applications_pkey", "uq_source_observation_application_entity"}, indexes)

	live := observationIDBySubject(t, pool, "live")
	require.True(t, retentionProtected(t, pool, live), "LIVE session 감사 보호는 코드 표 조인으로 유지된다")

	tag, err := pool.Exec(ctx, `INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		VALUES ($1,source_observation_entity_kind_id('youtube_live_session'),'vocab-live',source_observation_decision_id('REPLAY'))
		ON CONFLICT (observation_id,entity_kind_id,entity_key) DO NOTHING`, live)
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected(), "같은 원본·kind·key 감사는 코드 index가 중복을 막는다")

	tag, err = pool.Exec(ctx, `INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		VALUES ($1,source_observation_entity_kind_id('youtube_video'),'vocab-live',source_observation_decision_id('APPLIED'))`, live)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM source_observation_entity_kinds`).Scan(&kinds))
	require.Equal(t, 3, kinds, "새 어휘는 resolver가 사전에 추가한다")

	_, err = pool.Exec(ctx, `UPDATE youtube_live_sessions SET status='ENDED'`)
	require.NoError(t, err)
	require.False(t, retentionProtected(t, pool, live))

	_, err = pool.Exec(ctx, `DELETE FROM source_observation_entity_kinds WHERE name='youtube_live_session'`)
	require.Error(t, err, "참조 중인 어휘 코드는 삭제할 수 없다")

	_, err = pool.Exec(ctx, `DELETE FROM source_observations WHERE id=$1`, live)
	require.NoError(t, err)

	var remaining int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM source_observation_applications WHERE observation_id=$1`, live).Scan(&remaining))
	require.Zero(t, remaining, "원본 CASCADE는 유지된다")
}

func hexSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))

	return hex.EncodeToString(digest[:])
}
