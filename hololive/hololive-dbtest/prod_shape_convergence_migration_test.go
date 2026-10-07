package dbtest

import (
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const (
	lastPreConvergenceMigration      = "270_youtube_live_review_compact_comparison.sql"
	aclSettingsConvergenceMigration  = "273_acl_settings_shape_convergence.sql"
	membersConvergenceMigration      = "274_members_shape_convergence.sql"
	notificationConvergenceMigration = "275_notification_tables_shape_convergence.sql"
	retiredTablesDropMigration       = "276_drop_retired_prod_only_tables.sql"
	sqlStateForeignKeyViolation      = "23503"
	sqlStateRaiseException           = "P0001"
	membersTable                     = "members"
)

// 운영 drift를 수렴시키는 migration을 manifest 실행 순서대로 둔다.
var prodConvergenceMigrations = []string{
	"271_reassert_idle_transaction_timeout.sql",
	"272_drop_source_application_live_origin_index.sql",
	aclSettingsConvergenceMigration,
	membersConvergenceMigration,
	notificationConvergenceMigration,
	retiredTablesDropMigration,
}

// 2026-10-08 서울 운영 catalog를 golden과 같은 직렬화로 비교해 찾은 drift를 270 시점 DB에 재현한다.
// 열 순서와 CHECK 괄호 표기 차이는 의미가 같아 재현하지 않는다.
const prodDriftFixtureSQL = `
ALTER TABLE acl_settings DROP CONSTRAINT acl_settings_key_key;
ALTER TABLE acl_settings ALTER COLUMN key DROP NOT NULL, ALTER COLUMN id TYPE bigint;
ALTER SEQUENCE acl_settings_id_seq AS bigint;
CREATE UNIQUE INDEX idx_acl_settings_key ON acl_settings USING btree (key);
ALTER TABLE members
	ALTER COLUMN english_name TYPE varchar(100),
	ALTER COLUMN japanese_name TYPE varchar(100),
	ALTER COLUMN korean_name TYPE varchar(100),
	ALTER COLUMN is_graduated DROP NOT NULL,
	ALTER COLUMN aliases SET DEFAULT '{"ja": [], "ko": []}'::jsonb,
	ADD COLUMN created_at timestamp with time zone DEFAULT now(),
	ADD COLUMN updated_at timestamp with time zone DEFAULT now(),
	ADD CONSTRAINT check_aliases_structure CHECK (jsonb_typeof(aliases) = 'object' AND aliases ? 'ko' AND aliases ? 'ja'
		AND jsonb_typeof(aliases -> 'ko') = 'array' AND jsonb_typeof(aliases -> 'ja') = 'array'),
	ADD CONSTRAINT check_status CHECK (status = ANY (ARRAY[('active'::character varying)::text,
		('graduated'::character varying)::text, ('ended'::character varying)::text]));
ALTER TABLE notification_delivery_outbox ALTER COLUMN attempt_count TYPE bigint;
ALTER TABLE notification_template_revisions DROP CONSTRAINT notification_template_revisions_template_id_fkey;
` + prodYouTubeOutboxDriftSQL

// 운영 youtube_notification_outbox에만 있는 열과 status CHECK다.
const prodYouTubeOutboxDriftSQL = `
ALTER TABLE youtube_notification_outbox
	ADD COLUMN dispatched_at timestamp with time zone,
	ADD CONSTRAINT youtube_notification_outbox_status_check CHECK (status = ANY (ARRAY[('PENDING'::character varying)::text,
		('DISPATCHED'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text]));
`

// 운영에만 있는 빈 테이블 두 개와 각자 소유한 id 시퀀스다.
//
//nolint:misspell // 운영 alarm_dispatch_outbox의 실제 열·상태 철자(cancelled)를 바꾸면 운영 형태를 재현하지 못한다.
const prodRetiredTablesSQL = `
CREATE TABLE streams (
	id serial PRIMARY KEY,
	video_id text UNIQUE,
	channel_id text,
	title text,
	thumbnail text,
	status text,
	start_scheduled timestamp with time zone,
	start_actual timestamp with time zone,
	end_actual timestamp with time zone,
	topic text,
	org text,
	platform text,
	live_viewers bigint DEFAULT 0,
	created_at timestamp with time zone,
	updated_at timestamp with time zone,
	deleted_at timestamp with time zone
);
CREATE TABLE alarm_dispatch_outbox (
	id bigserial PRIMARY KEY,
	dedupe_key text NOT NULL,
	event_key text NOT NULL,
	payload_hash text NOT NULL DEFAULT '',
	room_id varchar(64) NOT NULL,
	channel_id varchar(64) NOT NULL DEFAULT '',
	alarm_type alarm_type NOT NULL DEFAULT 'LIVE',
	category text NOT NULL DEFAULT '',
	payload jsonb NOT NULL,
	claim_keys text[] NOT NULL DEFAULT ARRAY[]::text[],
	status text NOT NULL DEFAULT 'pending',
	attempt_count integer NOT NULL DEFAULT 0,
	next_attempt_at timestamp with time zone NOT NULL DEFAULT now(),
	locked_by text,
	locked_at timestamp with time zone,
	lock_expires_at timestamp with time zone,
	sending_started_at timestamp with time zone,
	sent_at timestamp with time zone,
	dlq_at timestamp with time zone,
	quarantined_at timestamp with time zone,
	cancelled_at timestamp with time zone,
	error text NOT NULL DEFAULT '',
	enqueued_at timestamp with time zone NOT NULL DEFAULT now(),
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	updated_at timestamp with time zone NOT NULL DEFAULT now(),
	CONSTRAINT alarm_dispatch_outbox_attempt_nonnegative CHECK (attempt_count >= 0),
	CONSTRAINT alarm_dispatch_outbox_status_check CHECK (status IN ('shadowed', 'pending', 'leased', 'retry', 'sending',
		'sent', 'dlq', 'quarantined', 'cancelled'))
);
CREATE UNIQUE INDEX ux_alarm_dispatch_outbox_dedupe ON alarm_dispatch_outbox USING btree (dedupe_key);
`

// 운영 제약을 만족하는 대표 행이다. 운영 전용 열에도 값을 넣고 DEFAULT 경로(is_graduated·aliases 생략)도 포함한다.
const prodShapedRowsSQL = `
INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases,
	org, sync_source, created_at, updated_at)
VALUES
	('prod-active', 'UC_prod_active', 'Prod Shape Active Member', 'プロド', '프로드', 'active', false,
	 '{"ko": ["프로드"], "ja": ["プロド"]}', 'Hololive', 'manual', '2025-01-02 03:04:05+00', '2025-02-03 04:05:06+00'),
	('prod-graduated', 'UC_prod_graduated', 'Prod Shape Graduate', NULL, NULL, 'graduated', true,
	 '{"ko": [], "ja": []}', 'Hololive', 'manual', '2024-01-02 03:04:05+00', '2024-02-03 04:05:06+00');
INSERT INTO members (slug, english_name, org, sync_source) VALUES ('prod-defaults', 'Prod Shape Defaults', 'Hololive', 'manual');
INSERT INTO acl_settings (key, value) VALUES ('enabled', 'true'), ('mode', 'whitelist');
INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, attempt_count)
VALUES ('NEW_VIDEO', 'UC_prod_active', 'prod-video-sent', '{}', 'SENT', 1),
	('LIVE_STREAM', 'UC_prod_active', 'prod-live-failed', '{}', 'FAILED', 3);
WITH template AS (
	INSERT INTO notification_templates (template_key, body) VALUES ('prod_shape_probe', 'probe body') RETURNING id, body
)
INSERT INTO notification_template_revisions (template_id, body) SELECT id, body FROM template;
`

const convergenceRowsSQL = `
SELECT jsonb_build_object(
	'members', (SELECT jsonb_agg(jsonb_build_array(slug, english_name, japanese_name, korean_name, status,
		is_graduated, aliases, xmin::text) ORDER BY slug) FROM members),
	'acl_settings', (SELECT jsonb_agg(jsonb_build_array(id, key, value, xmin::text) ORDER BY key) FROM acl_settings),
	'youtube_notification_outbox', (SELECT jsonb_agg(jsonb_build_array(id, kind, content_id, status, attempt_count,
		xmin::text) ORDER BY id) FROM youtube_notification_outbox),
	'notification_templates', (SELECT count(id) FROM notification_templates),
	'notification_template_revisions', (SELECT jsonb_agg(jsonb_build_array(id, template_id, xmin::text) ORDER BY id)
		FROM notification_template_revisions)
)::text`

const convergenceRelfilenodesSQL = `
SELECT jsonb_object_agg(relname, relfilenode)::text
FROM pg_class
WHERE oid IN ('members'::regclass, 'acl_settings'::regclass, 'notification_delivery_outbox'::regclass,
	'youtube_notification_outbox'::regclass, 'notification_template_revisions'::regclass)`

const idleTimeoutDatabaseSettingSQL = `
SELECT COALESCE((
	SELECT setting.config
	FROM pg_catalog.pg_db_role_setting AS database_setting
	CROSS JOIN LATERAL pg_catalog.unnest(database_setting.setconfig) AS setting(config)
	WHERE database_setting.setdatabase = (
		SELECT database_catalog.oid FROM pg_catalog.pg_database AS database_catalog
		WHERE database_catalog.datname = pg_catalog.current_database())
	  AND database_setting.setrole = 0
	  AND setting.config LIKE 'idle_in_transaction_session_timeout=%'), '')`

const unvalidatedConstraintsSQL = `
SELECT count(con.oid)
FROM pg_catalog.pg_constraint AS con
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = con.connamespace
WHERE namespace.nspname = current_schema() AND NOT con.convalidated`

type convergenceState struct {
	rows                   string
	relfilenodes           string
	idleTimeoutSetting     string
	unvalidatedConstraints int
	retiredSequences       int
}

// 운영 형태(270 적용 뒤 drift 재현)에 수렴 migration을 적용하면 기존 행과 테이블 저장소를 다시 쓰지 않고,
// 두 번째 적용은 아무것도 바꾸지 않으며, 남은 manifest까지 적용한 결과는 fresh 재생과 같은 스키마여야 한다.
func TestProdShapedSchemaConvergesToFreshReplay(t *testing.T) {
	ctx := t.Context()
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	want, err := serializeSchema(ctx, NewPool(t))
	require.NoError(t, err)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)

	split := slices.Index(entries, lastPreConvergenceMigration) + 1
	require.Positivef(t, split, "manifest must contain %s", lastPreConvergenceMigration)

	// 행·저장소 불변 단언은 수렴 파일만의 성질이다. 뒤에 추가되는 정상적인 데이터 변경 migration이 이 단언을
	// 깨지 않도록 271~276만 먼저 적용해 측정하고, 남은 항목은 최종 스키마 비교에만 쓴다.
	convergenceEnd := min(split+len(prodConvergenceMigrations), len(entries))
	require.Equal(t, prodConvergenceMigrations, entries[split:convergenceEnd],
		"manifest must list the convergence migrations right after %s", lastPreConvergenceMigration)

	prod := newProdShapedPool(t, dir, entries[:split])
	before := captureConvergenceState(t, prod)
	require.Empty(t, before.idleTimeoutSetting, "운영처럼 DB 수준 idle timeout이 없는 상태에서 시작해야 한다")
	require.Equal(t, 2, before.retiredSequences)

	for _, filename := range prodConvergenceMigrations {
		require.NoErrorf(t, applyManifestMigration(ctx, prod, dir, filename), "apply %s", filename)
	}

	after := captureConvergenceState(t, prod)
	require.Equal(t, before.rows, after.rows, "수렴은 기존 행 값과 행 버전을 바꾸지 않아야 한다")
	require.Equal(t, before.relfilenodes, after.relfilenodes, "운영 형태에서는 테이블을 다시 쓰지 않아야 한다")
	require.Equal(t, "idle_in_transaction_session_timeout=5min", after.idleTimeoutSetting)
	require.Zero(t, after.unvalidatedConstraints)
	require.Zero(t, after.retiredSequences, "운영 전용 테이블이 소유한 시퀀스도 함께 지워져야 한다")

	converged, err := serializeSchema(ctx, prod)
	require.NoError(t, err)

	// 중간 실패 뒤 러너는 파일 전체를 다시 적용하므로 이미 수렴한 DB에서도 그대로 성공하고 아무것도 바꾸지 않아야 한다.
	// 뒤 migration이 바꾼 모양에 수렴 파일의 가드가 반응하지 않도록 남은 항목보다 먼저 다시 적용한다.
	for _, filename := range prodConvergenceMigrations {
		require.NoErrorf(t, applyMigrationFile(ctx, prod, dir, filename), "reapply %s", filename)
	}

	requireSchemaMatches(t, prod, converged, "first convergence")
	require.Equal(t, after, captureConvergenceState(t, prod))

	for _, filename := range entries[convergenceEnd:] {
		require.NoErrorf(t, applyManifestMigration(ctx, prod, dir, filename), "apply %s", filename)
	}

	requireSchemaMatches(t, prod, want, "fresh replay")
}

func newProdShapedPool(t *testing.T, dir string, preConvergence []string) *pgxpool.Pool {
	t.Helper()

	ctx := t.Context()
	pool := NewBlankPool(t)
	require.NoError(t, ensureDBTestMigrationLedger(ctx, pool))

	for _, filename := range preConvergence {
		require.NoErrorf(t, applyManifestMigration(ctx, pool, dir, filename), "apply %s", filename)
	}

	_, err := pool.Exec(ctx, prodDriftFixtureSQL+prodRetiredTablesSQL+prodShapedRowsSQL)
	require.NoError(t, err)

	// 운영 DB에는 183이 남긴 DB 수준 기본값이 없다(2026-08-23 논리 복원에서 사라짐).
	_, err = pool.Exec(ctx, `DO $reset$ BEGIN
		EXECUTE format('ALTER DATABASE %I RESET idle_in_transaction_session_timeout', current_database());
	END $reset$`)
	require.NoError(t, err)

	return pool
}

func captureConvergenceState(t *testing.T, pool *pgxpool.Pool) convergenceState {
	t.Helper()

	ctx := t.Context()

	var state convergenceState

	require.NoError(t, pool.QueryRow(ctx, convergenceRowsSQL).Scan(&state.rows))
	require.NoError(t, pool.QueryRow(ctx, convergenceRelfilenodesSQL).Scan(&state.relfilenodes))
	require.NoError(t, pool.QueryRow(ctx, idleTimeoutDatabaseSettingSQL).Scan(&state.idleTimeoutSetting))
	require.NoError(t, pool.QueryRow(ctx, unvalidatedConstraintsSQL).Scan(&state.unvalidatedConstraints))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(oid) FROM pg_class
		WHERE oid IN (to_regclass('streams_id_seq'), to_regclass('alarm_dispatch_outbox_id_seq'))`).Scan(&state.retiredSequences))

	return state
}

func requireSchemaMatches(t *testing.T, pool *pgxpool.Pool, want, wantLabel string) {
	t.Helper()

	got, err := serializeSchema(t.Context(), pool)
	require.NoError(t, err)

	if got != want {
		t.Fatalf("schema differs from %s:\n%s", wantLabel, unifiedSchemaDiff(want, got))
	}
}

// NOT NULL·형식 검증이 실패하면 파일은 열과 제약을 바꾸기 전에 멈추고, 행을 고친 뒤 다시 적용하면 fresh와 같아진다.
func TestShapeConvergenceStopsOnInvalidRowsAndResumes(t *testing.T) {
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	want, err := serializeSchema(t.Context(), NewPool(t))
	require.NoError(t, err)

	const preMembersShapeSQL = `ALTER TABLE members DROP CONSTRAINT chk_members_aliases_shape,
		ALTER COLUMN aliases DROP NOT NULL, ALTER COLUMN aliases DROP DEFAULT, ALTER COLUMN is_graduated DROP NOT NULL;`

	const insertRejectedMember = `INSERT INTO members (slug, english_name, org, sync_source, aliases, is_graduated)
		VALUES ('rejected-row', 'Rejected Row', 'Hololive', 'manual', `

	for _, test := range []struct {
		name, file, setupSQL, repairSQL, pending, table, column string
	}{
		{
			name: "acl null key", file: aclSettingsConvergenceMigration,
			setupSQL:  `ALTER TABLE acl_settings ALTER COLUMN key DROP NOT NULL; INSERT INTO acl_settings (key, value) VALUES (NULL, 'rejected-row')`,
			repairSQL: `DELETE FROM acl_settings WHERE value = 'rejected-row'`,
			pending:   "acl_settings_key_nn", table: "acl_settings", column: "key",
		},
		{
			name: "member null graduation", file: membersConvergenceMigration,
			setupSQL:  preMembersShapeSQL + insertRejectedMember + `'{"ko": [], "ja": []}', NULL)`,
			repairSQL: `UPDATE members SET is_graduated = false WHERE slug = 'rejected-row'`,
			pending:   "members_is_graduated_nn", table: membersTable, column: "is_graduated",
		},
		{
			name: "member sql null aliases", file: membersConvergenceMigration,
			setupSQL:  preMembersShapeSQL + insertRejectedMember + `NULL, false)`,
			repairSQL: `UPDATE members SET aliases = '{"ko": [], "ja": []}' WHERE slug = 'rejected-row'`,
			pending:   "members_aliases_nn", table: membersTable, column: "aliases",
		},
		{
			name: "member json null aliases", file: membersConvergenceMigration,
			setupSQL:  preMembersShapeSQL + insertRejectedMember + `'null', false)`,
			repairSQL: `UPDATE members SET aliases = '{"ko": [], "ja": []}' WHERE slug = 'rejected-row'`,
			pending:   "chk_members_aliases_shape", table: membersTable, column: "aliases",
		},
		{
			name: "member aliases without ja", file: membersConvergenceMigration,
			setupSQL:  preMembersShapeSQL + insertRejectedMember + `'{"ko": ["별명"]}', false)`,
			repairSQL: `UPDATE members SET aliases = aliases || '{"ja": []}' WHERE slug = 'rejected-row'`,
			pending:   "chk_members_aliases_shape", table: membersTable, column: "aliases",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			pool := NewPool(t)

			_, err := pool.Exec(ctx, test.setupSQL)
			require.NoError(t, err)

			requireSQLState(t, applyMigrationFile(ctx, pool, dir, test.file), sqlStateCheckViolation)
			require.Falsef(t, constraintValidated(t, pool, test.table, test.pending), "%s must stay NOT VALID", test.pending)
			require.False(t, columnNotNull(t, pool, test.table, test.column), "SET NOT NULL must not run before validation")

			_, err = pool.Exec(ctx, test.repairSQL)
			require.NoError(t, err)
			require.NoError(t, applyMigrationFile(ctx, pool, dir, test.file))
			requireSchemaMatches(t, pool, want, "fresh replay")

			var unvalidated int

			require.NoError(t, pool.QueryRow(ctx, unvalidatedConstraintsSQL).Scan(&unvalidated))
			require.Zero(t, unvalidated)
		})
	}
}

// 고아 revision은 지우지 않는다. FK 검증이 실패해 적용이 멈추고, 고아를 정리한 뒤 다시 적용하면 검증된다.
func TestTemplateRevisionForeignKeyMigrationStopsOnOrphanRevision(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const constraint = "notification_template_revisions_template_id_fkey"

	_, err = pool.Exec(ctx, `ALTER TABLE notification_template_revisions DROP CONSTRAINT `+constraint+`;
		INSERT INTO notification_template_revisions (template_id, body) VALUES (-1, 'orphan revision')`)
	require.NoError(t, err)

	requireSQLState(t, applyMigrationFile(ctx, pool, dir, notificationConvergenceMigration), sqlStateForeignKeyViolation)
	require.False(t, constraintValidated(t, pool, "notification_template_revisions", constraint))

	var orphanKept bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM notification_template_revisions WHERE body = 'orphan revision')`).Scan(&orphanKept))
	require.True(t, orphanKept, "migration must not delete orphan revisions")

	_, err = pool.Exec(ctx, `DELETE FROM notification_template_revisions WHERE body = 'orphan revision'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, notificationConvergenceMigration))
	require.True(t, constraintValidated(t, pool, "notification_template_revisions", constraint))
}

// 운영 전용 dispatched_at에 값이 생겼으면 열을 지우지 않고 실패한다. 값을 정리한 뒤 다시 적용하면 fresh와 같아진다.
func TestNotificationConvergenceKeepsPopulatedDispatchedAt(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	want, err := serializeSchema(ctx, pool)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, prodYouTubeOutboxDriftSQL+`
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, attempt_count, dispatched_at)
		VALUES ('NEW_VIDEO', 'UC_dispatched', 'dispatched-video', '{}', 'SENT', 1, '2026-10-08 01:02:03+00')`)
	require.NoError(t, err)

	err = applyMigrationFile(ctx, pool, dir, notificationConvergenceMigration)
	requireSQLState(t, err, sqlStateRaiseException)
	require.ErrorContains(t, err, "youtube_notification_outbox.dispatched_at has values; column kept")

	var valueKept, statusCheckKept bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM youtube_notification_outbox
			WHERE content_id = 'dispatched-video' AND dispatched_at = '2026-10-08 01:02:03+00'),
		EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'youtube_notification_outbox'::regclass
			AND conname = 'youtube_notification_outbox_status_check')`).Scan(&valueKept, &statusCheckKept))
	require.True(t, valueKept, "migration must not drop a populated dispatched_at")
	require.True(t, statusCheckKept, "the failed transaction must not drop the status CHECK either")

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_outbox SET dispatched_at = NULL WHERE content_id = 'dispatched-video'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, notificationConvergenceMigration))
	requireSchemaMatches(t, pool, want, "fresh replay")
}

// 운영 전용 테이블에 행이 생겼으면 지우지 않고 실패하며 행을 보존한다.
func TestRetiredTableDropMigrationKeepsNonEmptyTable(t *testing.T) {
	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	_, err = pool.Exec(ctx, prodRetiredTablesSQL+`INSERT INTO streams (video_id) VALUES ('retired-stream')`)
	require.NoError(t, err)

	err = applyMigrationFile(ctx, pool, dir, retiredTablesDropMigration)
	requireSQLState(t, err, sqlStateRaiseException)
	require.ErrorContains(t, err, "retired table public.streams has rows; table kept")

	var kept, outboxKept bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM streams WHERE video_id = 'retired-stream'),
		to_regclass('alarm_dispatch_outbox') IS NOT NULL`).Scan(&kept, &outboxKept))
	require.True(t, kept)
	require.True(t, outboxKept, "the failed transaction must not drop the other retired table either")
}

func constraintValidated(t *testing.T, pool *pgxpool.Pool, table, constraint string) bool {
	t.Helper()

	var validated bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT convalidated FROM pg_constraint
		WHERE conrelid = to_regclass($1) AND conname = $2`, table, constraint).Scan(&validated))

	return validated
}

func columnNotNull(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()

	var notNull bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT attnotnull FROM pg_attribute
		WHERE attrelid = to_regclass($1) AND attname = $2 AND NOT attisdropped`, table, column).Scan(&notNull))

	return notNull
}
