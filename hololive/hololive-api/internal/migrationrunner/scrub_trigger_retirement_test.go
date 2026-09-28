package migrationrunner

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/scripts/migrations"
	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	beforeScrubRetirement = "229_drop_youtube_delivery_ledger_backfill_state.sql"
	scrubRetirementSQL    = "230_drop_bot_webhook_inbox_legacy_scrub_trigger.sql"
)

func TestScrubTriggerRetirementRejectsChangedTerminalCheck(t *testing.T) {
	pool := scrubRetirementPreMigrationPool(t)

	_, err := pool.Exec(t.Context(), `ALTER TABLE public.bot_webhook_inbox
		DROP CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `ALTER TABLE public.bot_webhook_inbox
		ADD CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed CHECK (payload IS NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}

	assertScrubRetirementRejected(t, pool, "terminal payload CHECK")
	assertTerminalPayloadScrubObjects(t, pool, true)
}

func TestScrubTriggerRetirementRejectsUnvalidatedTerminalCheck(t *testing.T) {
	pool := scrubRetirementPreMigrationPool(t)

	_, err := pool.Exec(t.Context(), `ALTER TABLE public.bot_webhook_inbox
		DROP CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `ALTER TABLE public.bot_webhook_inbox
		ADD CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed
		CHECK (status <> ALL (ARRAY['dead'::text, 'succeeded'::text]) OR payload = '{}'::jsonb) NOT VALID`)
	if err != nil {
		t.Fatal(err)
	}

	assertScrubRetirementRejected(t, pool, "terminal payload CHECK")
	assertTerminalPayloadScrubObjects(t, pool, true)
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", false)
}

func TestScrubTriggerRetirementRejectsUnknownMissingTrigger(t *testing.T) {
	pool := scrubRetirementPreMigrationPool(t)

	_, err := pool.Exec(t.Context(), `DROP TRIGGER bot_webhook_inbox_terminal_payload_scrub
		ON public.bot_webhook_inbox`)
	if err != nil {
		t.Fatal(err)
	}

	assertScrubRetirementRejected(t, pool, "absent or changed without 230 receipt")
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
}

func TestScrubTriggerRetirementRejectsChangedFunctionBody(t *testing.T) {
	pool := scrubRetirementPreMigrationPool(t)

	_, err := pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.scrub_bot_webhook_inbox_terminal_payload()
		RETURNS trigger LANGUAGE plpgsql AS $changed$
		BEGIN
			RETURN NEW;
		END
		$changed$`)
	if err != nil {
		t.Fatal(err)
	}

	assertScrubRetirementRejected(t, pool, "absent or changed without 230 receipt")
	assertTerminalPayloadScrubObjects(t, pool, true)
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
}

func TestScrubTriggerRetirementDependencyFailureRollsBack(t *testing.T) {
	pool := scrubRetirementPreMigrationPool(t)

	_, err := pool.Exec(t.Context(), `CREATE TABLE public.retained_scrub_dependency (payload jsonb)`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `CREATE TRIGGER retained_scrub_dependency
		BEFORE INSERT ON public.retained_scrub_dependency FOR EACH ROW
		EXECUTE FUNCTION public.scrub_bot_webhook_inbox_terminal_payload()`)
	if err != nil {
		t.Fatal(err)
	}

	assertScrubRetirementRejected(t, pool, "depend on it")
	assertTerminalPayloadScrubObjects(t, pool, true)
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)
}

func scrubRetirementPreMigrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := dbtest.NewBlankPool(t)
	runMigrations(t, pool, realManifestThrough(t, beforeScrubRetirement), "")

	assertTerminalPayloadScrubObjects(t, pool, true)
	assertConstraintValidated(t, pool, "bot_webhook_inbox", "chk_bot_webhook_inbox_terminal_payload_scrubbed", true)

	return pool
}

func assertScrubRetirementRejected(t *testing.T, pool *pgxpool.Pool, wantError string) {
	t.Helper()

	_, err := Run(t.Context(), pool, migrations.FS, Config{})
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("230 migration error = %v, want %q", err, wantError)
	}

	var recorded bool

	err = pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM public.schema_migrations WHERE filename = $1
	)`, scrubRetirementSQL).Scan(&recorded)
	if err != nil {
		t.Fatal(err)
	}

	if recorded {
		t.Fatal("failed 230 migration recorded an applied receipt")
	}
}
