package migrationrunner

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-api/scripts/migrations"
	"github.com/kapu/hololive-dbtest"
)

func TestFreshDBAppliesAllAndIgnoresBaselineWatermark(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
	}

	result := runMigrations(t, pool, fsys, "second.sql")
	if result.Applied != 2 || result.Skipped != 0 || result.Total != 2 {
		t.Fatalf("result = %+v, want applied=2 skipped=0 total=2", result)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql"})
	assertTablePresent(t, pool, "baseline_first_ran")
	assertTablePresent(t, pool, "baseline_second_ran")
}

func TestPopulatedDBEmptyLedgerNoBaselineRefuses(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	seedBaseSchema(t, pool)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want refusal on populated DB with empty ledger and no baseline")
	}
	if !strings.Contains(err.Error(), "empty schema_migrations ledger") {
		t.Fatalf("Run() error = %v, want empty-ledger refusal", err)
	}
	assertTableAbsent(t, pool, "baseline_first_ran")
	assertTableAbsent(t, pool, "baseline_second_ran")
}

func TestPopulatedDBEmptyLedgerBaselineStampsThenApplies(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	seedBaseSchema(t, pool)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n003 tail.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
		"tail.sql":             {Data: []byte("CREATE TABLE baseline_tail_ran(id integer)")},
	}

	result := runMigrations(t, pool, fsys, "second.sql")
	if result.Applied != 1 || result.Skipped != 2 || result.Total != 3 {
		t.Fatalf("result = %+v, want applied=1 skipped=2 total=3", result)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql", "tail.sql"})
	assertTableAbsent(t, pool, "baseline_first_ran")
	assertTableAbsent(t, pool, "baseline_second_ran")
	assertTablePresent(t, pool, "baseline_tail_ran")
	for _, name := range []string{"first.sql", "second.sql"} {
		var checksum string
		if err := pool.QueryRow(t.Context(), "SELECT checksum_sha256 FROM schema_migration_checksums WHERE filename = $1", name).Scan(&checksum); err != nil {
			t.Fatalf("read baseline checksum %s: %v", name, err)
		}
		if want := migrationChecksum(fsys[name].Data); checksum != want {
			t.Fatalf("baseline checksum %s = %s, want %s", name, checksum, want)
		}
	}
}

func TestBeginWrappedFileFailureRollsBackWholeTxBlock(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_atomic_probe(id integer);\nSELECT 1/0;\nCOMMIT;\n")},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want failure from statement inside BEGIN block")
	}
	assertTableAbsent(t, pool, "tx_atomic_probe")
}

func TestEpoch2BaselineLateFailureRollsBackAllObjectsLedgerAndPrivileges(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	const runtimeRole = "pg_monitor"

	for _, statement := range []string{
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + runtimeRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + runtimeRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + runtimeRole,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("configure runtime-equivalent default privileges: %v", err)
		}
	}

	failureFS := realManifestThrough(t, epoch2Baseline)
	baselineSQL := append([]byte(nil), failureFS[epoch2Baseline].Data...)
	failureFS[epoch2Baseline].Data = injectBeforeFinalCommit(t, baselineSQL,
		"SELECT public.epoch2_injected_late_failure();")

	if _, err := Run(ctx, pool, failureFS, Config{}); err == nil {
		t.Fatal("late-failure baseline Run() error = nil")
	}
	assertReplayAuditAbsent(t, pool, runtimeRole)
	assertTableAbsent(t, pool, "members")
	assertMigrationNotRecorded(t, pool, epoch2Baseline)

	failureFS[epoch2Baseline].Data = baselineSQL
	result, err := Run(ctx, pool, failureFS, Config{})
	if err != nil {
		t.Fatalf("rerun baseline after rollback: %v", err)
	}
	if result.Applied != 1 || result.Skipped != 0 || result.Total != 1 {
		t.Fatalf("rerun result = %+v, want applied=1 skipped=0 total=1", result)
	}
	assertReplayAuditSealed(t, pool, runtimeRole)
}

func injectBeforeFinalCommit(t *testing.T, migrationSQL []byte, statement string) []byte {
	t.Helper()
	const finalCommit = "\nCOMMIT;"
	index := strings.LastIndex(string(migrationSQL), finalCommit)
	if index < 0 {
		t.Fatal("migration is not wrapped by a final COMMIT")
	}
	injected := append([]byte(nil), migrationSQL[:index]...)
	injected = append(injected, []byte("\n"+statement+"\nCOMMIT;")...)
	injected = append(injected, migrationSQL[index+len(finalCommit):]...)
	return injected
}

func assertReplayAuditAbsent(t *testing.T, pool *pgxpool.Pool, runtimeRole string) {
	t.Helper()
	ctx := t.Context()
	var tablePresent, grantFunctionPresent, claimFunctionPresent, mutationFunctionPresent bool
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass('public.bot_reply_outbox_replay_audit') IS NOT NULL,
		to_regprocedure('public.grant_bot_reply_outbox_manual_replay(bigint,text,text)') IS NOT NULL,
		to_regprocedure('public.append_bot_reply_outbox_replay_claim_audit()') IS NOT NULL,
		to_regprocedure('public.reject_bot_reply_outbox_replay_audit_mutation()') IS NOT NULL`).Scan(
		&tablePresent, &grantFunctionPresent, &claimFunctionPresent, &mutationFunctionPresent); err != nil {
		t.Fatalf("inspect rolled-back migration 136 objects: %v", err)
	}
	if tablePresent || grantFunctionPresent || claimFunctionPresent || mutationFunctionPresent {
		t.Fatalf("migration 136 left partial objects: table=%v grant=%v claim=%v mutation=%v",
			tablePresent, grantFunctionPresent, claimFunctionPresent, mutationFunctionPresent)
	}

	var triggerCount, privilegeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
		WHERE tgname IN ('bot_reply_outbox_replay_audit_immutable', 'bot_reply_outbox_replay_claim_audit')`).Scan(&triggerCount); err != nil {
		t.Fatalf("count rolled-back migration 136 triggers: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT 1 FROM information_schema.role_table_grants
		WHERE grantee = $1 AND table_schema = 'public' AND table_name = 'bot_reply_outbox_replay_audit'
		UNION ALL
		SELECT 1 FROM information_schema.role_usage_grants
		WHERE grantee = $1 AND object_schema = 'public' AND object_name = 'bot_reply_outbox_replay_audit_id_seq'
		UNION ALL
		SELECT 1 FROM information_schema.routine_privileges
		WHERE grantee = $1 AND specific_schema = 'public'
		  AND routine_name IN (
		      'grant_bot_reply_outbox_manual_replay',
		      'append_bot_reply_outbox_replay_claim_audit',
		      'reject_bot_reply_outbox_replay_audit_mutation'
		  )
	) AS exposed`, runtimeRole).Scan(&privilegeCount); err != nil {
		t.Fatalf("count rolled-back migration 136 privileges: %v", err)
	}
	if triggerCount != 0 || privilegeCount != 0 {
		t.Fatalf("migration 136 left trigger/ACL exposure: triggers=%d privileges=%d", triggerCount, privilegeCount)
	}
}

func assertMigrationNotRecorded(t *testing.T, pool *pgxpool.Pool, migrationName string) {
	t.Helper()
	var ledgered, checksummed bool
	if err := pool.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1),
		EXISTS (SELECT 1 FROM schema_migration_checksums WHERE filename = $1)`, migrationName).Scan(
		&ledgered, &checksummed); err != nil {
		t.Fatalf("inspect failed migration ledger: %v", err)
	}
	if ledgered || checksummed {
		t.Fatalf("failed migration recorded: ledger=%v checksum=%v", ledgered, checksummed)
	}
}

func assertReplayAuditSealed(t *testing.T, pool *pgxpool.Pool, runtimeRole string) {
	t.Helper()
	ctx := t.Context()
	var tablePresent, grantFunctionPresent bool
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass('public.bot_reply_outbox_replay_audit') IS NOT NULL,
		to_regprocedure('public.grant_bot_reply_outbox_manual_replay(bigint,text,text)') IS NOT NULL`).Scan(
		&tablePresent, &grantFunctionPresent); err != nil {
		t.Fatalf("inspect migration 136 objects: %v", err)
	}
	if !tablePresent || !grantFunctionPresent {
		t.Fatalf("migration 136 objects missing: table=%v grant_function=%v", tablePresent, grantFunctionPresent)
	}

	var triggerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
		WHERE tgname IN ('bot_reply_outbox_replay_audit_immutable', 'bot_reply_outbox_replay_claim_audit')`).Scan(&triggerCount); err != nil {
		t.Fatalf("count migration 136 triggers: %v", err)
	}
	if triggerCount != 2 {
		t.Fatalf("migration 136 trigger count = %d, want 2", triggerCount)
	}

	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		var allowed bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege($1, 'public.bot_reply_outbox_replay_audit', $2)`,
			runtimeRole, privilege).Scan(&allowed); err != nil {
			t.Fatalf("inspect runtime table privilege %s: %v", privilege, err)
		}
		if allowed {
			t.Errorf("runtime-equivalent role retained audit table privilege %s", privilege)
		}
	}
	var sequenceAllowed, functionAllowed bool
	if err := pool.QueryRow(ctx, `SELECT
		has_sequence_privilege($1, 'public.bot_reply_outbox_replay_audit_id_seq', 'USAGE'),
		has_function_privilege($1, 'public.grant_bot_reply_outbox_manual_replay(bigint,text,text)', 'EXECUTE')`,
		runtimeRole).Scan(&sequenceAllowed, &functionAllowed); err != nil {
		t.Fatalf("inspect runtime sequence/function privileges: %v", err)
	}
	if sequenceAllowed || functionAllowed {
		t.Errorf("runtime-equivalent role retained audit sequence/function privileges: sequence=%v function=%v",
			sequenceAllowed, functionAllowed)
	}
}

func TestDropAndAddConstraintStatementIsAtomicOnAddFailure(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	_, err := pool.Exec(t.Context(), `
		CREATE TABLE constraint_atomic_probe(status text NOT NULL);
		ALTER TABLE constraint_atomic_probe ADD CONSTRAINT old_status_check CHECK (status IN ('old'));
		INSERT INTO constraint_atomic_probe VALUES ('old')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `ALTER TABLE constraint_atomic_probe
		DROP CONSTRAINT old_status_check,
		ADD CONSTRAINT new_status_check CHECK (status IN ('new'))`)
	if err == nil {
		t.Fatal("ALTER TABLE error = nil, want failed validation")
	}
	var oldExists, newExists bool
	err = pool.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'constraint_atomic_probe'::regclass AND conname = 'old_status_check'),
		EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'constraint_atomic_probe'::regclass AND conname = 'new_status_check')`).Scan(&oldExists, &newExists)
	if err != nil {
		t.Fatal(err)
	}
	if !oldExists || newExists {
		t.Fatalf("constraints after failure: old=%v new=%v", oldExists, newExists)
	}
}

func TestMigration114PreflightIgnoresSameNamedObjectsOutsidePublic(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	_, err := pool.Exec(t.Context(), `
		CREATE TABLE public.members(slug text);
		CREATE SCHEMA shadow;
		CREATE TABLE shadow.members(slug text CONSTRAINT members_slug_key UNIQUE);
		CREATE INDEX idx_members_name_trgm ON shadow.members(slug)`)
	if err != nil {
		t.Fatal(err)
	}
	var indexDefinition *string
	err = pool.QueryRow(t.Context(), `SELECT pg_get_indexdef(c.oid)
		FROM (SELECT 'idx_members_name_trgm'::text AS name) e
		LEFT JOIN pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
		ON c.relkind = 'i' AND c.relname = e.name`).Scan(&indexDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if indexDefinition != nil {
		t.Fatalf("shadow index satisfied public lookup: %q", *indexDefinition)
	}
	var publicConstraint bool
	err = pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_constraint
		WHERE conname = 'members_slug_key' AND conrelid = 'public.members'::regclass)`).Scan(&publicConstraint)
	if err != nil {
		t.Fatal(err)
	}
	if publicConstraint {
		t.Fatal("shadow constraint satisfied public lookup")
	}
}

func TestAppliedMigrationChecksumMismatchFails(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	first := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 immutable.sql\n")},
		"immutable.sql":        {Data: []byte("CREATE TABLE immutable_v1(id integer)")},
	}
	if _, err := Run(t.Context(), pool, first, Config{}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	modified := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 immutable.sql\n")},
		"immutable.sql":        {Data: []byte("CREATE TABLE immutable_v2(id integer)")},
	}
	_, err := Run(t.Context(), pool, modified, Config{})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("second Run() error = %v, want checksum mismatch", err)
	}
}

func TestFailedMigrationDoesNotPinBadChecksum(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	broken := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 repairable.sql\n")},
		"repairable.sql":       {Data: []byte("SELECT 1/0")},
	}
	if _, err := Run(t.Context(), pool, broken, Config{}); err == nil {
		t.Fatal("broken migration error = nil")
	}

	fixed := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 repairable.sql\n")},
		"repairable.sql":       {Data: []byte("CREATE TABLE repaired_after_failure(id integer)")},
	}
	if _, err := Run(t.Context(), pool, fixed, Config{}); err != nil {
		t.Fatalf("fixed migration error = %v", err)
	}
	assertTablePresent(t, pool, "repaired_after_failure")
}

func TestAppliedLedgerEntryMissingChecksumFailsClosed(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO schema_migrations(filename) VALUES ('legacy.sql')
	`); err != nil {
		t.Fatalf("seed legacy ledger: %v", err)
	}

	content := []byte("SELECT 1")
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 legacy.sql\n")},
		"legacy.sql":           {Data: content},
	}
	_, err := Run(ctx, pool, fsys, Config{})
	if err == nil || !strings.Contains(err.Error(), "checksum is missing") {
		t.Fatalf("Run() error = %v, want missing-checksum refusal", err)
	}

	var checksumPresent bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migration_checksums WHERE filename = 'legacy.sql')").Scan(&checksumPresent); err != nil {
		t.Fatalf("inspect missing checksum: %v", err)
	}
	if checksumPresent {
		t.Fatal("missing checksum was silently backfilled")
	}
}

func TestBeginWrappedFileAppliesTxBlockAndTrailingAutocommit(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("-- header comment\nBEGIN;\nCREATE TABLE tx_inside_ran(id integer);\nCOMMIT;\nCREATE TABLE tx_after_ran(id integer);\n")},
	}

	result, err := Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("result = %+v, want applied=1", result)
	}
	assertLedger(t, pool, []string{"tx.sql"})
	assertTablePresent(t, pool, "tx_inside_ran")
	assertTablePresent(t, pool, "tx_after_ran")
}

func TestBeginWrappedFileTrailingAutocommitFailureCanBeRerun(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_committed_probe(id integer);\nCOMMIT;\nSELECT * FROM tx_missing_probe;\n")},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want trailing autocommit failure")
	}
	assertTablePresent(t, pool, "tx_committed_probe")
	assertTableAbsent(t, pool, "tx_tail_ran")
	assertLedger(t, pool, nil)

	fsys["tx.sql"] = &fstest.MapFile{Data: []byte("BEGIN;\nCREATE TABLE IF NOT EXISTS tx_committed_probe(id integer);\nCOMMIT;\nCREATE TABLE tx_tail_ran(id integer);\n")}
	result, err := Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() rerun error = %v", err)
	}
	if result.Applied != 1 || result.Skipped != 0 || result.Total != 1 {
		t.Fatalf("rerun result = %+v, want applied=1 skipped=0 total=1", result)
	}
	assertTablePresent(t, pool, "tx_committed_probe")
	assertTablePresent(t, pool, "tx_tail_ran")
	assertLedger(t, pool, []string{"tx.sql"})
}

func TestCurrentSchemaSupportsLegacyTerminalWriter(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	runMigrations(t, pool, migrations.FS, "")
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
	assertTerminalPayloadScrubTrigger(t, pool)
	assertLegacyTerminalWriterCompatible(t, pool, "legacy-succeeded", "succeeded")
	assertLegacyTerminalWriterCompatible(t, pool, "legacy-dead", "dead")
}

func TestBeginWrappedFileWithConcurrentlyIsRejected(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_conc_probe(id integer);\nCREATE INDEX CONCURRENTLY tx_conc_idx ON tx_conc_probe(id);\nCOMMIT;\n")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want explicit CONCURRENTLY-inside-BEGIN rejection")
	}
	if !strings.Contains(err.Error(), "CONCURRENTLY") {
		t.Fatalf("Run() error = %v, want CONCURRENTLY rejection", err)
	}
	assertTableAbsent(t, pool, "tx_conc_probe")
}

func TestBeginWrappedFileMissingCommitIsRejected(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_unclosed_probe(id integer);\n")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want missing-COMMIT rejection")
	}
	assertTableAbsent(t, pool, "tx_unclosed_probe")
}

func TestRunAppliesSessionTimeoutsToAllSegments(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 probe.sql\n")},
		"probe.sql": {Data: []byte(`BEGIN;
CREATE TABLE session_probe_tx AS
SELECT setting::bigint AS lock_timeout_ms FROM pg_settings WHERE name = 'lock_timeout';
COMMIT;
CREATE TABLE session_probe AS
SELECT
  (SELECT setting::bigint FROM pg_settings WHERE name = 'lock_timeout') AS lock_timeout_ms,
  (SELECT setting::bigint FROM pg_settings WHERE name = 'statement_timeout') AS statement_timeout_ms;
`)},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var lockMs, stmtMs int64
	if err := pool.QueryRow(t.Context(), "SELECT lock_timeout_ms, statement_timeout_ms FROM session_probe").Scan(&lockMs, &stmtMs); err != nil {
		t.Fatalf("read session probe: %v", err)
	}
	if lockMs != 10_000 {
		t.Errorf("autocommit segment lock_timeout = %dms, want 10000ms", lockMs)
	}
	if stmtMs != 240_000 {
		t.Errorf("autocommit segment statement_timeout = %dms, want 240000ms", stmtMs)
	}

	var txLockMs int64
	if err := pool.QueryRow(t.Context(), "SELECT lock_timeout_ms FROM session_probe_tx").Scan(&txLockMs); err != nil {
		t.Fatalf("read tx session probe: %v", err)
	}
	if txLockMs != 10_000 {
		t.Errorf("tx segment lock_timeout = %dms, want 10000ms", txLockMs)
	}
}

func TestRealManifestFullReplayOnBlankDB(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	entries := manifestEntries(t)
	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	t.Logf("full replay result: %d applied / %d skipped (total %d)", result.Applied, result.Skipped, result.Total)

	if result.Applied != len(entries) || result.Skipped != 0 || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=%d skipped=0 total=%d", result, len(entries), len(entries))
	}
	assertTablePresent(t, pool, "members")
	assertTablePresent(t, pool, "alarms")
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
	assertTerminalPayloadScrubTrigger(t, pool)

	result, err = Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if result.Applied != 0 || result.Skipped != len(entries) || result.Total != len(entries) {
		t.Fatalf("second result = %+v, want applied=0 skipped=%d total=%d", result, len(entries), len(entries))
	}
}

func TestRealManifestCheckpointedAt140SkipsBaselineAndAppliesSuffix(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	baselineFS := realManifestThrough(t, epoch2Baseline)
	runMigrations(t, pool, baselineFS, "")
	if _, err := pool.Exec(t.Context(), "DELETE FROM schema_migration_checksums WHERE filename = $1", epoch2Baseline); err != nil {
		t.Fatalf("remove pre-R2 baseline checksum fixture: %v", err)
	}
	prefillEpoch2LegacyContract(t, pool)

	entries := manifestEntries(t)
	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied != len(entries)-1 || result.Skipped != 1 || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=%d skipped=1 total=%d", result, len(entries)-1, len(entries))
	}
	assertMigrationRecorded(t, pool, "179_alarm_dispatch_collab_members.sql", true)
}

func TestRealManifestPartialAt160AppliesOnlyRemainingSuffix(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	const partialTip = "160_youtube_live_reconciliation_candidate_fk_index.sql"
	partialFS := realManifestThrough(t, partialTip)
	partialEntries, err := dbmigrate.Manifest(partialFS)
	if err != nil {
		t.Fatalf("read partial manifest: %v", err)
	}
	runMigrations(t, pool, partialFS, "")
	prefillEpoch2LegacyContract(t, pool)

	entries := manifestEntries(t)
	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied != len(entries)-len(partialEntries) || result.Skipped != len(partialEntries) || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=%d skipped=%d total=%d", result, len(entries)-len(partialEntries), len(partialEntries), len(entries))
	}
	assertMigrationRecorded(t, pool, "179_alarm_dispatch_collab_members.sql", true)
}

func TestRealManifestFullyCurrentWithLegacyResidueSkipsAll(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	entries := manifestEntries(t)
	runMigrations(t, pool, migrations.FS, "")
	prefillEpoch2LegacyContract(t, pool)

	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied != 0 || result.Skipped != len(entries) || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=0 skipped=%d total=%d", result, len(entries), len(entries))
	}
}

func TestRealManifestEditedBaselineFailsChecksum(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	runMigrations(t, pool, migrations.FS, "")
	entries := manifestEntries(t)
	editedFS := realManifestThrough(t, entries[len(entries)-1])
	editedFS[epoch2Baseline].Data = append(editedFS[epoch2Baseline].Data, []byte("\n-- edited after exposure\n")...)

	_, err := Run(t.Context(), pool, editedFS, Config{})
	if err == nil || !strings.Contains(err.Error(), epoch2Baseline+" checksum mismatch") {
		t.Fatalf("Run() error = %v, want immutable baseline checksum mismatch", err)
	}
}

func TestR1RollbackIgnoresR2LedgerResidue(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	legacyFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 legacy.sql\n002 checkpoint.sql\n")},
		"legacy.sql":           {Data: []byte("CREATE TABLE rollback_legacy_state(id integer)")},
		"checkpoint.sql":       {Data: []byte("SELECT 1")},
	}
	runMigrations(t, pool, legacyFS, "")
	prefillLedger(t, pool, []string{epoch2Baseline, "141_suffix.sql"})
	if _, err := pool.Exec(t.Context(), mustSQL("ensure_migration_checksums.sql")); err != nil {
		t.Fatalf("ensure checksum ledger: %v", err)
	}
	for _, name := range []string{epoch2Baseline, "141_suffix.sql"} {
		if _, err := pool.Exec(t.Context(), mustSQL("record_migration_checksum.sql"), name, strings.Repeat("a", 64)); err != nil {
			t.Fatalf("record R2 residue checksum %s: %v", name, err)
		}
	}

	result, err := Run(t.Context(), pool, legacyFS, Config{})
	if err != nil {
		t.Fatalf("R1 rollback Run() error = %v", err)
	}
	if result.Applied != 0 || result.Skipped != 2 || result.Total != 2 {
		t.Fatalf("rollback result = %+v, want applied=0 skipped=2 total=2", result)
	}
	assertTablePresent(t, pool, "rollback_legacy_state")
}

func TestRealManifestPrefilledLedgerWithoutChecksumsRefuses(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	entries := manifestEntries(t)
	prefillLedger(t, pool, entries)

	_, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err == nil || !strings.Contains(err.Error(), "recorded without its checksum") {
		t.Fatalf("Run() error = %v, want unproven epoch marker refusal", err)
	}
	assertTableAbsent(t, pool, "members")
}

func TestLedgerResidueWithoutEpochBaselineRefuses(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	legacyFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 legacy.sql\n")},
		"legacy.sql":           {Data: []byte("CREATE TABLE legacy_epoch_ran(id integer)")},
	}
	runMigrations(t, pool, legacyFS, "")

	epochFS := fstest.MapFS{
		dbmigrate.ManifestName:           {Data: []byte("001 001_schema_epoch2_baseline.sql\n")},
		"001_schema_epoch2_baseline.sql": {Data: []byte("BEGIN;\nCREATE TABLE epoch2_baseline_ran(id integer);\nCOMMIT;\n")},
	}
	_, err := Run(t.Context(), pool, epochFS, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want refusal on ledger residue without recorded epoch baseline")
	}
	if !strings.Contains(err.Error(), "epoch baseline") {
		t.Fatalf("Run() error = %v, want epoch-baseline refusal", err)
	}
	assertTableAbsent(t, pool, "epoch2_baseline_ran")

	_, err = Run(t.Context(), pool, epochFS, Config{BaselineThrough: "001_schema_epoch2_baseline.sql"})
	if err == nil || !strings.Contains(err.Error(), "epoch baseline") {
		t.Fatalf("Run() with BaselineThrough error = %v, want residue refusal to resist the watermark knob", err)
	}
	assertTableAbsent(t, pool, "epoch2_baseline_ran")
}

func TestLedgerResidueWithChecksummedEpochBaselineSkipsBaseline(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	const testBaseline = "001_schema_test_baseline.sql"

	legacyFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 legacy.sql\n002 checkpoint.sql\n")},
		"legacy.sql":           {Data: []byte("CREATE TABLE legacy_epoch_ran(id integer)")},
		"checkpoint.sql":       {Data: []byte("INSERT INTO schema_migrations (filename) VALUES ('" + testBaseline + "') ON CONFLICT (filename) DO NOTHING")},
	}
	runMigrations(t, pool, legacyFS, "")

	epochFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 " + testBaseline + "\n")},
		testBaseline:           {Data: []byte("CREATE TABLE epoch2_baseline_ran(id integer)")},
	}
	if _, err := pool.Exec(t.Context(), mustSQL("ensure_migration_checksums.sql")); err != nil {
		t.Fatalf("ensure checksum ledger: %v", err)
	}
	if _, err := pool.Exec(
		t.Context(),
		mustSQL("record_migration_checksum.sql"),
		testBaseline,
		migrationChecksum(epochFS[testBaseline].Data),
	); err != nil {
		t.Fatalf("record epoch baseline checksum: %v", err)
	}
	result, err := Run(t.Context(), pool, epochFS, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v, want checkpointed DB to proceed", err)
	}
	if result.Applied != 0 || result.Skipped != 1 || result.Total != 1 {
		t.Fatalf("result = %+v, want applied=0 skipped=1 total=1", result)
	}
	assertTableAbsent(t, pool, "epoch2_baseline_ran")
}

func TestEpoch2LegacyContractAllowsCheckpointedLedger(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	prefillEpoch2LegacyContract(t, pool)
	prefillLedger(t, pool, []string{epoch2Baseline})

	epochFS := epoch2BaselineProbeFS()
	result, err := Run(t.Context(), pool, epochFS, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v, want checkpointed contract to pass", err)
	}
	if result.Applied != 0 || result.Skipped != 1 || result.Total != 1 {
		t.Fatalf("result = %+v, want applied=0 skipped=1 total=1", result)
	}
	assertTableAbsent(t, pool, "epoch2_baseline_ran")

	var checksum string
	if err := pool.QueryRow(t.Context(), "SELECT checksum_sha256 FROM schema_migration_checksums WHERE filename = $1", epoch2Baseline).Scan(&checksum); err != nil {
		t.Fatalf("read backfilled baseline checksum: %v", err)
	}
	if checksum != migrationChecksum(epochFS[epoch2Baseline].Data) {
		t.Fatalf("baseline checksum = %s, want current source checksum", checksum)
	}
}

func TestEpoch2LegacyContractRejectsMissingLedger(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	prefillEpoch2LegacyContract(t, pool)
	prefillLedger(t, pool, []string{epoch2Baseline})
	missing := mustEpoch2LegacyContract(t)[0].name
	if _, err := pool.Exec(t.Context(), "DELETE FROM schema_migrations WHERE filename = $1", missing); err != nil {
		t.Fatalf("remove legacy ledger fixture: %v", err)
	}
	assertEpoch2ContractRefusal(t, pool, missing+" is missing from schema_migrations")
}

func TestEpoch2LegacyContractRejectsMissingChecksum(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	prefillEpoch2LegacyContract(t, pool)
	prefillLedger(t, pool, []string{epoch2Baseline})
	missing := mustEpoch2LegacyContract(t)[0].name
	if _, err := pool.Exec(t.Context(), "DELETE FROM schema_migration_checksums WHERE filename = $1", missing); err != nil {
		t.Fatalf("remove legacy checksum fixture: %v", err)
	}
	assertEpoch2ContractRefusal(t, pool, missing+" checksum is missing")
}

func TestEpoch2LegacyContractRejectsChecksumMismatch(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	prefillEpoch2LegacyContract(t, pool)
	prefillLedger(t, pool, []string{epoch2Baseline})
	mismatch := mustEpoch2LegacyContract(t)[0].name
	if _, err := pool.Exec(t.Context(), "UPDATE schema_migration_checksums SET checksum_sha256 = repeat('0', 64) WHERE filename = $1", mismatch); err != nil {
		t.Fatalf("mutate legacy checksum fixture: %v", err)
	}
	assertEpoch2ContractRefusal(t, pool, mismatch+" checksum mismatch")
}

func epoch2BaselineProbeFS() fstest.MapFS {
	return fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 " + epoch2Baseline + "\n")},
		epoch2Baseline:         {Data: []byte("BEGIN;\nCREATE TABLE epoch2_baseline_ran(id integer);\nCOMMIT;\n")},
	}
}

func mustEpoch2LegacyContract(t *testing.T) []epoch2LegacyMigration {
	t.Helper()
	contract, err := parseEpoch2LegacyContract(epoch2LegacyContractRaw)
	if err != nil {
		t.Fatalf("parse epoch-2 contract: %v", err)
	}
	return contract
}

func prefillEpoch2LegacyContract(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	if _, err := pool.Exec(ctx, mustSQL("ensure_migration_checksums.sql")); err != nil {
		t.Fatalf("create checksum ledger fixture: %v", err)
	}
	contract := mustEpoch2LegacyContract(t)
	names := make([]string, len(contract))
	for index, migration := range contract {
		names[index] = migration.name
		if _, err := pool.Exec(ctx, mustSQL("record_migration_checksum.sql"), migration.name, migration.checksum); err != nil {
			t.Fatalf("prefill legacy checksum %s: %v", migration.name, err)
		}
	}
	prefillLedger(t, pool, names)
}

func assertEpoch2ContractRefusal(t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()
	_, err := Run(t.Context(), pool, epoch2BaselineProbeFS(), Config{})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Run() error = %v, want %q", err, want)
	}
	assertTableAbsent(t, pool, "epoch2_baseline_ran")
}

func TestEpochLedgerWithoutResidueProceeds(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	epochFS := fstest.MapFS{
		dbmigrate.ManifestName:           {Data: []byte("001 001_schema_epoch2_baseline.sql\n")},
		"001_schema_epoch2_baseline.sql": {Data: []byte("BEGIN;\nCREATE TABLE epoch2_baseline_ran(id integer);\nCOMMIT;\n")},
	}
	runMigrations(t, pool, epochFS, "")

	nextFS := fstest.MapFS{
		dbmigrate.ManifestName:           {Data: []byte("001 001_schema_epoch2_baseline.sql\n002 002_next.sql\n")},
		"001_schema_epoch2_baseline.sql": {Data: []byte("BEGIN;\nCREATE TABLE epoch2_baseline_ran(id integer);\nCOMMIT;\n")},
		"002_next.sql":                   {Data: []byte("CREATE TABLE epoch2_next_ran(id integer)")},
	}
	result, err := Run(t.Context(), pool, nextFS, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v, want residue-free epoch DB to proceed", err)
	}
	if result.Applied != 1 || result.Skipped != 1 || result.Total != 2 {
		t.Fatalf("result = %+v, want applied=1 skipped=1 total=2", result)
	}
	assertTablePresent(t, pool, "epoch2_next_ran")
}

func manifestEntries(t *testing.T) []string {
	t.Helper()

	entries, err := dbmigrate.Manifest(migrations.FS)
	if err != nil {
		t.Fatalf("read embedded manifest: %v", err)
	}
	return entries
}

func realManifestThrough(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	entries := manifestEntries(t)
	fake := make(fstest.MapFS)
	var manifest strings.Builder
	found := false
	for index, name := range entries {
		raw, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read real migration %s: %v", name, err)
		}
		fake[name] = &fstest.MapFile{Data: raw}
		fmt.Fprintf(&manifest, "%03d %s\n", index+1, name)
		if name == last {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("real migration %s not found", last)
	}
	fake[dbmigrate.ManifestName] = &fstest.MapFile{Data: []byte(manifest.String())}
	return fake
}

func prefillLedger(t *testing.T, pool *pgxpool.Pool, entries []string) {
	t.Helper()

	ctx := t.Context()
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"); err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	for _, name := range entries {
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations(filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING", name); err != nil {
			t.Fatalf("prefill ledger %s: %v", name, err)
		}
	}
}

func seedBaseSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, ddl := range []string{
		"CREATE TABLE members(id integer)",
		"CREATE TABLE alarms(id integer)",
	} {
		if _, err := pool.Exec(t.Context(), ddl); err != nil {
			t.Fatalf("seed base schema: %v", err)
		}
	}
}

func runMigrations(t *testing.T, pool *pgxpool.Pool, fsys fs.FS, baselineThrough string) Result {
	t.Helper()

	result, err := Run(t.Context(), pool, fsys, Config{BaselineThrough: baselineThrough})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return result
}

func assertLedger(t *testing.T, pool *pgxpool.Pool, want []string) {
	t.Helper()

	rows, err := pool.Query(t.Context(), "SELECT filename FROM schema_migrations ORDER BY filename")
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			t.Fatalf("scan ledger: %v", scanErr)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("ledger = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ledger = %v, want %v", got, want)
		}
	}
}

func assertConstraintValidated(t *testing.T, pool *pgxpool.Pool, table, constraint string, want bool) {
	t.Helper()

	var got bool
	err := pool.QueryRow(t.Context(), `
		SELECT convalidated
		FROM pg_constraint
		WHERE conrelid = $1::regclass
		  AND conname = $2`, table, constraint).Scan(&got)
	if err != nil {
		t.Fatalf("query constraint %s on %s: %v", constraint, table, err)
	}
	if got != want {
		t.Fatalf("constraint %s on %s convalidated = %v, want %v", constraint, table, got, want)
	}
}

func assertTerminalPayloadScrubTrigger(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var exists bool
	err := pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1
			FROM pg_trigger t
			JOIN pg_proc p ON p.oid = t.tgfoid
			WHERE t.tgrelid = 'bot_webhook_inbox'::regclass
			  AND t.tgname = 'bot_webhook_inbox_terminal_payload_scrub'
			  AND NOT t.tgisinternal
			  AND t.tgenabled = 'O'
			  AND t.tgqual IS NOT NULL
			  AND p.proname = 'scrub_bot_webhook_inbox_terminal_payload'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query terminal payload scrub trigger: %v", err)
	}
	if !exists {
		t.Fatal("terminal payload scrub trigger is not installed and enabled")
	}
}

func assertInboxPayload(t *testing.T, pool *pgxpool.Pool, messageID, want string) {
	t.Helper()

	var got string
	if err := pool.QueryRow(t.Context(), "SELECT payload::text FROM bot_webhook_inbox WHERE message_id = $1", messageID).Scan(&got); err != nil {
		t.Fatalf("query inbox payload for %s: %v", messageID, err)
	}
	if got != want {
		t.Fatalf("inbox payload for %s = %s, want %s", messageID, got, want)
	}
}

func assertLegacyTerminalWriterCompatible(t *testing.T, pool *pgxpool.Pool, messageID, status string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bot_webhook_inbox(message_id, room_id, ordering_key, payload)
		VALUES ($1, 'room', 'room', '{"message":"retained"}'::jsonb)`, messageID); err != nil {
		t.Fatalf("legacy writer insert for %s: %v", status, err)
	}
	assertInboxPayload(t, pool, messageID, `{"message": "retained"}`)
	updateSQL := "UPDATE bot_webhook_inbox SET status = $1 WHERE message_id = $2"
	if status == "dead" {
		updateSQL = `UPDATE bot_webhook_inbox
			SET status = $1, terminal_at = clock_timestamp(), terminal_reason = 'legacy terminal failure'
			WHERE message_id = $2`
	}
	if _, err := pool.Exec(t.Context(), updateSQL, status, messageID); err != nil {
		t.Fatalf("legacy writer %s update: %v", status, err)
	}
	assertInboxPayload(t, pool, messageID, "{}")
}

func assertTablePresent(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	if !tableExists(t, pool, name) {
		t.Fatalf("table %s missing", name)
	}
}

func assertTableAbsent(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	if tableExists(t, pool, name) {
		t.Fatalf("table %s present", name)
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}
