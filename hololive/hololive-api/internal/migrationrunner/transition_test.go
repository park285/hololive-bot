package migrationrunner

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/scripts/migrations"
	dbtest "github.com/kapu/hololive-dbtest"
)

func epoch2HeadFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.NewBlankPool(t)
	runMigrations(t, pool, realManifestThrough(t, epoch3Baseline), "")
	_, err := pool.Exec(t.Context(), `-- 로그인은 외부 세션 생성 부작용이 있어, 프로세스 종료 후에도 시도와 결과 불명을 보존합니다.
CREATE TABLE IF NOT EXISTS x_space_login_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    configuration_revision BIGINT NOT NULL CHECK (configuration_revision > 0),
    session_revision BIGINT NOT NULL CHECK (session_revision >= 0),
    submitted_revision BIGINT,
    status TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    CONSTRAINT chk_x_space_login_attempts_status_vocab CHECK (
        status IN ('running', 'submitted', 'connected', 'login_required', 'outcome_unknown', 'manual_override')
    ),
    CONSTRAINT chk_x_space_login_attempts_error_code_vocab CHECK (
        error_code IN ('', 'additional_authentication', 'login_rejected', 'browser_failed',
                       'outcome_unknown', 'interrupted', 'candidate_rejected', 'manual_override')
    ),
    CONSTRAINT uq_x_space_login_attempts_generation UNIQUE (configuration_revision, session_revision)
);
-- 최근 로그인 횟수 조회가 장기 운영의 전체 원장을 스캔하지 않게 합니다.
CREATE INDEX IF NOT EXISTS idx_x_space_login_attempts_started_at ON x_space_login_attempts (started_at);

 INSERT INTO x_space_login_attempts(configuration_revision,session_revision,status) VALUES (1,0,'outcome_unknown');
 DELETE FROM schema_migration_checksums;
 DELETE FROM schema_migrations;`)
	require.NoError(t, err)
	for line := range strings.SplitSeq(strings.TrimSpace(epoch2HeadSeal), "\n") {
		checksum, filename, ok := strings.Cut(line, "  ")
		require.True(t, ok)
		_, err := pool.Exec(t.Context(), `INSERT INTO schema_migrations(filename) VALUES ($1)`, filename)
		require.NoError(t, err)
		_, err = pool.Exec(t.Context(), mustSQL("record_migration_checksum.sql"), filename, checksum)
		require.NoError(t, err)
	}
	return pool
}

func TestEpoch2HeadTransitionMatchesFreshSchema(t *testing.T) {
	entries := manifestEntries(t)
	old := epoch2HeadFixture(t)
	_, err := old.Exec(t.Context(), `UPDATE notification_templates SET body='운영 사용자 본문' WHERE template_key='CMD_HELP' AND channel_id IS NULL AND format_variant='plain'`)
	require.NoError(t, err)
	result, err := Run(t.Context(), old, migrations.FS, Config{})
	require.NoError(t, err)
	require.Equal(t, Result{Applied: len(entries) - 1, Skipped: 1, Total: len(entries)}, result)
	assertLedger(t, old, entries)
	assertTableAbsent(t, old, "x_space_login_attempts")
	var body string
	require.NoError(t, old.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key='CMD_HELP' AND channel_id IS NULL AND format_variant='plain'`).Scan(&body))
	require.Equal(t, "운영 사용자 본문", body)
	fresh := dbtest.NewBlankPool(t)
	runMigrations(t, fresh, migrations.FS, "")
	want, err := dbtest.SnapshotSchema(t.Context(), fresh)
	require.NoError(t, err)
	got, err := dbtest.SnapshotSchema(t.Context(), old)
	require.NoError(t, err)
	require.Equal(t, want, got)
	result, err = Run(t.Context(), old, migrations.FS, Config{})
	require.NoError(t, err)
	require.Equal(t, Result{Skipped: len(entries), Total: len(entries)}, result)
}

func TestEpoch2TransitionRejectsUnsealedReceiptsBeforeSQL(t *testing.T) {
	for name, corrupt := range map[string]string{
		"partial":          `DELETE FROM schema_migrations WHERE filename='372_help_broadcast_time_sum.sql'`,
		"unknown":          `INSERT INTO schema_migrations(filename) VALUES ('unknown.sql')`,
		"epoch1":           `INSERT INTO schema_migrations(filename) VALUES ('140_epoch2_checkpoint.sql')`,
		"checksum":         `UPDATE schema_migration_checksums SET checksum_sha256=repeat('a',64) WHERE filename='001_schema_epoch2_baseline.sql'`,
		"missing_checksum": `DELETE FROM schema_migration_checksums WHERE filename='001_schema_epoch2_baseline.sql'`,
		"orphan_checksum":  `INSERT INTO schema_migration_checksums(filename,checksum_sha256) VALUES ('orphan.sql',repeat('a',64))`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := epoch2HeadFixture(t)
			_, err := pool.Exec(t.Context(), corrupt)
			require.NoError(t, err)
			var before string
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_agg(filename ORDER BY filename)::text FROM schema_migrations`).Scan(&before))
			_, err = Run(t.Context(), pool, migrations.FS, Config{BaselineThrough: epoch3Baseline})
			require.ErrorContains(t, err, "upgrade to 372 with the previous release")
			var after string
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_agg(filename ORDER BY filename)::text FROM schema_migrations`).Scan(&after))
			require.Equal(t, before, after)
			assertMigrationNotRecorded(t, pool, epoch3Baseline)
			assertTablePresent(t, pool, "x_space_login_attempts")
		})
	}
}

func TestEpochTransitionReceiptFailureRollsBackBothLedgers(t *testing.T) {
	pool := epoch2HeadFixture(t)
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_epoch3_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.filename='001_schema_epoch3_baseline.sql' THEN RAISE EXCEPTION 'receipt rejected'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_epoch3_receipt BEFORE INSERT ON schema_migrations FOR EACH ROW EXECUTE FUNCTION reject_epoch3_receipt();`)
	require.NoError(t, err)
	_, err = Run(t.Context(), pool, migrations.FS, Config{})
	require.ErrorContains(t, err, "receipt rejected")
	var ledgerCount, checksumCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM schema_migration_checksums)`).Scan(&ledgerCount, &checksumCount))
	require.Equal(t, 233, ledgerCount)
	require.Equal(t, 233, checksumCount)
	assertTablePresent(t, pool, "x_space_login_attempts")
}

func TestEpochTransitionDropFailureCanResumeWithoutBaselineReplay(t *testing.T) {
	total := len(manifestEntries(t))
	pool := epoch2HeadFixture(t)
	_, err := pool.Exec(t.Context(), `CREATE VIEW login_attempt_dependency AS SELECT id FROM x_space_login_attempts`)
	require.NoError(t, err)
	_, err = Run(t.Context(), pool, migrations.FS, Config{})
	require.ErrorContains(t, err, "depend")
	assertLedger(t, pool, []string{epoch3Baseline})
	assertTablePresent(t, pool, "x_space_login_attempts")
	_, err = pool.Exec(t.Context(), `DROP VIEW login_attempt_dependency`)
	require.NoError(t, err)
	result, err := Run(t.Context(), pool, migrations.FS, Config{})
	require.NoError(t, err)
	require.Equal(t, Result{Applied: total - 1, Skipped: 1, Total: total}, result)
	assertTableAbsent(t, pool, "x_space_login_attempts")
}

func TestEpoch3RecoveryWatermarkUsesCurrentManifest(t *testing.T) {
	total := len(manifestEntries(t))
	pool := dbtest.NewBlankPool(t)
	runMigrations(t, pool, realManifestThrough(t, epoch3Baseline), "")
	_, err := pool.Exec(t.Context(), `DELETE FROM schema_migration_checksums; DELETE FROM schema_migrations`)
	require.NoError(t, err)
	_, err = Run(t.Context(), pool, migrations.FS, Config{BaselineThrough: "001_schema_epoch2_baseline.sql"})
	require.ErrorContains(t, err, "not a manifest migration")
	result, err := Run(t.Context(), pool, migrations.FS, Config{BaselineThrough: epoch3Baseline})
	require.NoError(t, err)
	require.Equal(t, Result{Applied: total - 1, Skipped: 1, Total: total}, result)
}
