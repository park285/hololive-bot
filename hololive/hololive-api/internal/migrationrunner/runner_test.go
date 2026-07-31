package migrationrunner

import (
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

func TestMigration136LateFailureRollsBackObjectsLedgerAndPrivileges(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	const (
		migrationName = "136_reply_outbox_manual_replay_audit.sql"
		replayName    = "136_reply_outbox_manual_replay_audit_replay.sql"
		runtimeRole   = "pg_monitor"
	)

	for _, statement := range []string{
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + runtimeRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + runtimeRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + runtimeRole,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("configure runtime-equivalent default privileges: %v", err)
		}
	}

	entries := manifestEntries(t)
	manifestSQL, err := fs.ReadFile(migrations.FS, dbmigrate.ManifestName)
	if err != nil {
		t.Fatalf("read real migration manifest: %v", err)
	}
	failureFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: manifestSQL},
	}
	var migrationSQL []byte
	for _, name := range entries {
		raw, readErr := fs.ReadFile(migrations.FS, name)
		if readErr != nil {
			t.Fatalf("read embedded migration %s: %v", name, readErr)
		}
		if name == migrationName {
			migrationSQL = raw
			raw = injectBeforeFinalCommit(t, raw,
				"SELECT public.migration_136_injected_late_failure();")
		}
		failureFS[name] = &fstest.MapFile{Data: raw}
	}
	if len(migrationSQL) == 0 {
		t.Fatalf("%s missing from real manifest", migrationName)
	}

	if _, err := Run(ctx, pool, failureFS, Config{}); err == nil {
		t.Fatal("late-failure migration Run() error = nil")
	}
	assertMigration136Absent(t, pool, runtimeRole)
	assertMigrationNotRecorded(t, pool, migrationName)

	failureFS[migrationName] = &fstest.MapFile{Data: migrationSQL}
	result, err := Run(ctx, pool, failureFS, Config{})
	if err != nil {
		t.Fatalf("rerun migration 136 after rollback: %v", err)
	}
	if result.Applied != 1 || result.Skipped != len(entries)-1 || result.Total != len(entries) {
		t.Fatalf("rerun result = %+v, want applied=1 skipped=%d total=%d", result, len(entries)-1, len(entries))
	}
	assertMigration136Sealed(t, pool, runtimeRole)

	replayManifest := append([]byte(nil), manifestSQL...)
	if len(replayManifest) > 0 && replayManifest[len(replayManifest)-1] != '\n' {
		replayManifest = append(replayManifest, '\n')
	}
	replayManifest = append(replayManifest, []byte("133 "+replayName+"\n")...)
	failureFS[dbmigrate.ManifestName] = &fstest.MapFile{Data: replayManifest}
	failureFS[replayName] = &fstest.MapFile{Data: migrationSQL}
	result, err = Run(ctx, pool, failureFS, Config{})
	if err != nil {
		t.Fatalf("idempotent migration 136 replay: %v", err)
	}
	if result.Applied != 1 || result.Skipped != len(entries) || result.Total != len(entries)+1 {
		t.Fatalf("idempotent replay result = %+v, want applied=1 skipped=%d total=%d",
			result, len(entries), len(entries)+1)
	}
	assertMigration136Sealed(t, pool, runtimeRole)
}

func injectBeforeFinalCommit(t *testing.T, migrationSQL []byte, statement string) []byte {
	t.Helper()
	const finalCommit = "\nCOMMIT;"
	index := strings.LastIndex(string(migrationSQL), finalCommit)
	if index < 0 {
		t.Fatal("migration 136 is not wrapped by a final COMMIT")
	}
	injected := append([]byte(nil), migrationSQL[:index]...)
	injected = append(injected, []byte("\n"+statement+"\nCOMMIT;")...)
	injected = append(injected, migrationSQL[index+len(finalCommit):]...)
	return injected
}

func assertMigration136Absent(t *testing.T, pool *pgxpool.Pool, runtimeRole string) {
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

func assertMigration136Sealed(t *testing.T, pool *pgxpool.Pool, runtimeRole string) {
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

func TestAppliedLedgerEntryBackfillsMissingChecksum(t *testing.T) {
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
	result, err := Run(ctx, pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("result = %+v, want one skipped migration", result)
	}

	var checksum string
	if err := pool.QueryRow(ctx, "SELECT checksum_sha256 FROM schema_migration_checksums WHERE filename = 'legacy.sql'").Scan(&checksum); err != nil {
		t.Fatalf("load backfilled checksum: %v", err)
	}
	if want := migrationChecksum(content); checksum != want {
		t.Fatalf("checksum = %q, want %q", checksum, want)
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

func TestMigration133SupportsPopulatedPreviousSchemaAndLegacyWriter(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	const previousMigrationName = "123_create_bot_durable_admission_tables.sql"
	const migrationName = "133_webhook_inbox_terminal_payload_check.sql"
	previousMigrationSQL, err := fs.ReadFile(migrations.FS, previousMigrationName)
	if err != nil {
		t.Fatalf("read embedded migration %s: %v", previousMigrationName, err)
	}
	migrationSQL, err := fs.ReadFile(migrations.FS, migrationName)
	if err != nil {
		t.Fatalf("read embedded migration %s: %v", migrationName, err)
	}
	previousFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("119 " + previousMigrationName + "\n")},
		previousMigrationName:  {Data: previousMigrationSQL},
	}
	result, err := Run(t.Context(), pool, previousFS, Config{})
	if err != nil {
		t.Fatalf("apply previous schema: %v", err)
	}
	if result.Applied != 1 || result.Skipped != 0 || result.Total != 1 {
		t.Fatalf("previous schema result = %+v, want applied=1 skipped=0 total=1", result)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bot_webhook_inbox(message_id, room_id, ordering_key, payload, status)
		VALUES ('existing-terminal', 'room', 'room', '{"message":"retained"}'::jsonb, 'succeeded')`); err != nil {
		t.Fatalf("seed previous-schema terminal row: %v", err)
	}

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("119 " + previousMigrationName + "\n129 " + migrationName + "\n")},
		previousMigrationName:  {Data: previousMigrationSQL},
		migrationName:          {Data: migrationSQL},
	}

	result, err = Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("apply migration 133: %v", err)
	}
	if result.Applied != 1 || result.Skipped != 1 || result.Total != 2 {
		t.Fatalf("migration 133 result = %+v, want applied=1 skipped=1 total=2", result)
	}
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
	assertTerminalPayloadScrubTrigger(t, pool)
	assertInboxPayload(t, pool, "existing-terminal", "{}")
	assertLedger(t, pool, []string{previousMigrationName, migrationName})

	assertLegacyTerminalWriterCompatible(t, pool, "legacy-succeeded", "succeeded")
	assertLegacyTerminalWriterCompatible(t, pool, "legacy-dead", "dead")

	result, err = Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("rerun migration 133: %v", err)
	}
	if result.Applied != 0 || result.Skipped != 2 || result.Total != 2 {
		t.Fatalf("rerun result = %+v, want applied=0 skipped=2 total=2", result)
	}
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
}

func TestRealManifestPrefilledLedgerSkipsAll(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	entries := manifestEntries(t)
	prefillLedger(t, pool, entries)

	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	t.Logf("prefilled ledger result: %d applied / %d skipped (total %d)", result.Applied, result.Skipped, result.Total)

	if result.Applied != 0 || result.Skipped != len(entries) || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=0 skipped=%d total=%d", result, len(entries), len(entries))
	}
}

func manifestEntries(t *testing.T) []string {
	t.Helper()

	entries, err := dbmigrate.Manifest(migrations.FS)
	if err != nil {
		t.Fatalf("read embedded manifest: %v", err)
	}
	return entries
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
