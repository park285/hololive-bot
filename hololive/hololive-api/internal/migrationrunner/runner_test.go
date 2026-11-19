package migrationrunner

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/scripts/migrations"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/migrationsource"
)

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

func TestEpoch4BaselineLateFailureRollsBackAllObjectsLedgerAndPrivileges(t *testing.T) {
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

	files, err := migrationsource.Load(migrations.FS)
	require.NoError(t, err)

	brokenSQL := strings.TrimSuffix(strings.TrimSpace(string(files[0].SQL)), "COMMIT;") + "SELECT public.epoch4_injected_late_failure();\nCOMMIT;"
	broken := fstest.MapFS{"manifest.txt": {Data: []byte("001 " + migrationsource.Baseline)}, migrationsource.Baseline: {Data: []byte(brokenSQL)}, "contracts/" + migrationsource.Baseline: {Data: []byte(files[0].Contract)}}

	if _, runErr := Run(ctx, pool, broken, Config{}); runErr == nil {
		t.Fatal("late-failure baseline Run() error = nil")
	}

	assertReplayAuditAbsent(t, pool, runtimeRole)
	assertTableAbsent(t, pool, "members")
	assertTableAbsent(t, pool, "schema_migrations")
	assertTableAbsent(t, pool, "schema_migration_checksums")

	result, err := Run(ctx, pool, migrations.FS, Config{})
	if err != nil {
		t.Fatalf("rerun baseline after rollback: %v", err)
	}

	if result.Applied != 1 || result.Skipped != 0 || result.Total != 1 {
		t.Fatalf("rerun result = %+v, want applied=1 skipped=0 total=1", result)
	}

	assertReplayAuditSealed(t, pool, runtimeRole)
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

func TestCurrentSchemaRejectsTerminalPayloadRetention(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	_, err := Run(t.Context(), pool, migrations.FS, Config{})
	require.NoError(t, err)

	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
	assertLegacyTerminalWriterRejected(t, pool, "legacy-succeeded", "succeeded")
	assertLegacyTerminalWriterRejected(t, pool, "legacy-dead", "dead")
}

func TestRealManifestFullReplayOnBlankDB(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	const entries = 1

	result, err := Run(t.Context(), pool, migrations.FS, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	t.Logf("full replay result: %d applied / %d skipped (total %d)", result.Applied, result.Skipped, result.Total)

	if result.Applied != entries || result.Skipped != 0 || result.Total != entries {
		t.Fatalf("result = %+v, want applied=%d skipped=0 total=%d", result, entries, entries)
	}

	assertTablePresent(t, pool, "members")
	assertTablePresent(t, pool, "alarms")
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)

	result, err = Run(t.Context(), pool, migrations.FS, Config{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if result.Applied != 0 || result.Skipped != entries || result.Total != entries {
		t.Fatalf("second result = %+v, want applied=0 skipped=%d total=%d", result, entries, entries)
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

func assertLegacyTerminalWriterRejected(t *testing.T, pool *pgxpool.Pool, messageID, status string) {
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
		if !strings.Contains(err.Error(), "chk_bot_webhook_inbox_terminal_payload_scrubbed") {
			t.Fatalf("legacy writer %s update error = %v, want terminal payload CHECK", status, err)
		}
	} else {
		t.Fatalf("legacy writer %s update unexpectedly succeeded", status)
	}

	assertInboxPayload(t, pool, messageID, `{"message": "retained"}`)
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

func TestAdvisoryLockSessionEvictClosesConnAndReleasesLock(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()

	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock holder: %v", err)
	}
	defer held.Release()

	holder := pgxAdvisoryLockSession{conn: held}

	if acquired, lockErr := holder.TryAdvisoryLock(ctx, AdvisoryLockKey); lockErr != nil || !acquired {
		t.Fatalf("holder TryAdvisoryLock() = %v, %v; want true", acquired, lockErr)
	}

	var holderPID int32

	if pidErr := held.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); pidErr != nil {
		t.Fatalf("holder backend pid: %v", pidErr)
	}

	if evictErr := holder.EvictAfterUnlockFailure(); evictErr != nil {
		t.Fatalf("EvictAfterUnlockFailure() error = %v", evictErr)
	}

	other, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire second session: %v", err)
	}
	defer other.Release()

	exitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for {
		var alive bool

		if queryErr := other.QueryRow(exitCtx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1)", holderPID).Scan(&alive); queryErr != nil {
			t.Fatalf("query holder backend: %v", queryErr)
		}

		if !alive {
			break
		}
	}

	contender := pgxAdvisoryLockSession{conn: other}

	acquired, err := contender.TryAdvisoryLock(ctx, AdvisoryLockKey)
	if err != nil || !acquired {
		t.Fatalf("contender TryAdvisoryLock() = %v, %v; want true after eviction", acquired, err)
	}

	if _, err := contender.AdvisoryUnlock(ctx, AdvisoryLockKey); err != nil {
		t.Fatalf("contender AdvisoryUnlock() error = %v", err)
	}
}

func TestUntrackedDatabaseRemainsUntouched(t *testing.T) {
	for name, setup := range map[string]string{
		"relation":       "CREATE TABLE orphan(id integer)",
		"function":       "CREATE FUNCTION orphan() RETURNS int LANGUAGE sql AS 'SELECT 1'",
		"schema":         "CREATE SCHEMA abandoned",
		"ledger only":    "CREATE TABLE schema_migrations(filename text PRIMARY KEY)",
		"checksums only": "CREATE TABLE schema_migration_checksums(filename text PRIMARY KEY)",
	} {
		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewBlankPool(t)
			_, err := pool.Exec(t.Context(), setup)
			require.NoError(t, err)

			_, err = Run(t.Context(), pool, migrations.FS, Config{BeforeInitialize: func(context.Context) error {
				t.Fatal("login roles bootstrapped before admission")

				return nil
			}})
			require.Error(t, err)
			assertTableAbsent(t, pool, "members")

			if name != "ledger only" {
				assertTableAbsent(t, pool, "schema_migrations")
			}

			if name != "checksums only" {
				assertTableAbsent(t, pool, "schema_migration_checksums")
			}
		})
	}
}
