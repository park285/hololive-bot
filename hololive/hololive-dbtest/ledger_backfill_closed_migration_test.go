package dbtest

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ledgerBackfillClosedMigration    = "227_youtube_delivery_ledger_backfill_closed.sql"
	ledgerBackfillStateDropMigration = "229_drop_youtube_delivery_ledger_backfill_state.sql"
	insertRetiredLedgerState         = `
INSERT INTO youtube_notification_delivery_ledger_state (
    singleton, schema_version, delivery_high_water_id, outbox_high_water_id,
    delivery_cursor_id, delivery_verify_cursor_id, outbox_cursor_id,
    legacy_coverage_start_at, coverage_verified_at, started_at, completed_at, updated_at
) VALUES (true, 1, 0, 0, 0, 0, 0, now(), now(), now(), %s, now())`
)

// migration 227은 alarm-worker의 ledger 완료 gate(ensureReady)를 대신해 적용 시점에 backfill 완료를 확인한다
// (DEC-20260926-hololive-retired-rollback-tooling). 완료되지 않은 state나 backfill 없이 190을 지난 DB는 거절하고,
// 빈 DB와 완료된 운영 DB는 통과해야 한다.
func TestLedgerBackfillClosedMigrationGuardsCompletion(t *testing.T) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		setup   string
		wantErr string
	}{
		{name: "empty database", setup: ""},
		{name: "completed state", setup: strings.Replace(insertRetiredLedgerState, "%s", "now()", 1)},
		{
			name:    "incomplete state",
			setup:   strings.Replace(insertRetiredLedgerState, "%s", "NULL", 1),
			wantErr: "ledger backfill is not complete",
		},
		{
			name: "outbox rows without state",
			setup: `INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
VALUES ('NEW_VIDEO', 'UC0000000000000000000000', 'video-226', '{}'::jsonb)`,
			wantErr: "ledger backfill never ran",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := NewPool(t)
			ctx := t.Context()

			// Full manifest의 229가 state를 폐기했으므로 227 단독 재생용 과거 상태를 만든다.
			createRetiredLedgerStateFixture(t, pool)

			if tc.setup != "" {
				if _, err := pool.Exec(ctx, tc.setup); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			err := applyMigrationFile(ctx, pool, dir, ledgerBackfillClosedMigration)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("apply %s: %v", ledgerBackfillClosedMigration, err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("apply %s error = %v, want %q", ledgerBackfillClosedMigration, err, tc.wantErr)
			}
		})
	}
}

func TestLedgerBackfillStateDroppedAfterCompletion(t *testing.T) {
	var stateExists, ledgerExists bool

	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationFile(t.Context(), pool, dir, ledgerBackfillStateDropMigration); err != nil {
		t.Fatalf("recorded 229 reapply: %v", err)
	}

	if err := pool.QueryRow(t.Context(), `SELECT to_regclass('youtube_notification_delivery_ledger_state') IS NOT NULL,
		to_regclass('youtube_notification_delivery_ledger') IS NOT NULL`).Scan(&stateExists, &ledgerExists); err != nil {
		t.Fatal(err)
	}
	if stateExists || !ledgerExists {
		t.Fatalf("state exists=%t, logical ledger exists=%t; want false,true", stateExists, ledgerExists)
	}
}

func createRetiredLedgerStateFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `CREATE TABLE youtube_notification_delivery_ledger_state (
		singleton boolean, schema_version integer, delivery_high_water_id bigint,
		outbox_high_water_id bigint, delivery_cursor_id bigint,
		delivery_verify_cursor_id bigint, outbox_cursor_id bigint,
		legacy_coverage_start_at timestamptz, coverage_verified_at timestamptz,
		started_at timestamptz, completed_at timestamptz, updated_at timestamptz
	)`)
	if err != nil {
		t.Fatalf("create retired state fixture: %v", err)
	}
}

func createProductionMigrationLedgerFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `CREATE TABLE public.schema_migrations (
		filename text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		t.Fatalf("create production migration ledger fixture: %v", err)
	}
	_, err = pool.Exec(t.Context(), `INSERT INTO public.schema_migrations (filename) VALUES ($1)`, ledgerBackfillClosedMigration)
	if err != nil {
		t.Fatalf("record 227 fixture: %v", err)
	}
}

// 229는 227 과거 기록뿐 아니라 현재 state를 잠금 아래 재검증하고, DROP 실패 시 전체를 되돌린다.
func TestLedgerBackfillStateDropRequiresCurrentCompletion(t *testing.T) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		setup     string
		fixture   bool
		remove227 bool
		wantErr   string
	}{
		{name: "unknown missing table", wantErr: "absent without 229 receipt"},
		{name: "fresh empty bootstrap", fixture: true},
		{name: "completed singleton", fixture: true, setup: strings.Replace(insertRetiredLedgerState, "%s", "now()", 1)},
		{name: "missing 227 receipt", fixture: true, remove227: true, setup: strings.Replace(insertRetiredLedgerState, "%s", "now()", 1), wantErr: "requires applied 227 receipt"},
		{name: "incomplete singleton", fixture: true, setup: strings.Replace(insertRetiredLedgerState, "%s", "NULL", 1), wantErr: "incomplete or malformed"},
		{name: "malformed version", fixture: true, setup: strings.Replace(strings.Replace(insertRetiredLedgerState, "%s", "now()", 1), "VALUES (true, 1,", "VALUES (true, 2,", 1), wantErr: "incomplete or malformed"},
		{name: "malformed null singleton", fixture: true, setup: strings.Replace(strings.Replace(insertRetiredLedgerState, "%s", "now()", 1), "VALUES (true, 1,", "VALUES (NULL, 1,", 1), wantErr: "incomplete or malformed"},
		{name: "missing singleton with data", fixture: true, setup: `INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
			VALUES ('NEW_VIDEO', 'UC0000000000000000000000', 'video-229', '{}'::jsonb)`, wantErr: "no singleton"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var exists bool

			pool := NewPool(t)
			ctx := t.Context()

			if _, err := pool.Exec(ctx, `DELETE FROM hololive_dbtest_internal.schema_migrations
				WHERE filename = $1`, ledgerBackfillStateDropMigration); err != nil {
				t.Fatal(err)
			}

			if tc.remove227 {
				if _, err := pool.Exec(ctx, `DELETE FROM hololive_dbtest_internal.schema_migrations
					WHERE filename = $1`, ledgerBackfillClosedMigration); err != nil {
					t.Fatal(err)
				}
			}

			if tc.fixture {
				createRetiredLedgerStateFixture(t, pool)
			}

			if tc.setup != "" {
				if _, err := pool.Exec(ctx, tc.setup); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			err := applyMigrationFile(ctx, pool, dir, ledgerBackfillStateDropMigration)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("229 error = %v, want %q", err, tc.wantErr)
			}

			if err := pool.QueryRow(ctx, `SELECT to_regclass('youtube_notification_delivery_ledger_state') IS NOT NULL`).Scan(&exists); err != nil {
				t.Fatal(err)
			}

			if exists != (tc.fixture && tc.wantErr != "") {
				t.Fatalf("state remains = %t, want %t", exists, tc.fixture && tc.wantErr != "")
			}
		})
	}
}

func TestLedgerBackfillStateDropRollsBackOnDependency(t *testing.T) {
	var stateExists, viewExists, receiptExists bool

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	pool := NewPool(t)
	ctx := t.Context()

	createProductionMigrationLedgerFixture(t, pool)
	createRetiredLedgerStateFixture(t, pool)
	if _, err := pool.Exec(ctx, strings.Replace(insertRetiredLedgerState, "%s", "now()", 1)); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `CREATE VIEW retained_ledger_state_dependency AS
		SELECT schema_version FROM youtube_notification_delivery_ledger_state`); err != nil {
		t.Fatal(err)
	}
	err = applyMigrationFile(ctx, pool, dir, ledgerBackfillStateDropMigration)
	if err == nil || !strings.Contains(err.Error(), "depend on it") {
		t.Fatalf("dependent DROP error = %v, want dependency refusal", err)
	}

	if err := pool.QueryRow(ctx, `SELECT to_regclass('youtube_notification_delivery_ledger_state') IS NOT NULL,
		to_regclass('retained_ledger_state_dependency') IS NOT NULL,
		EXISTS (SELECT 1 FROM public.schema_migrations WHERE filename = $1)`, ledgerBackfillStateDropMigration).
		Scan(&stateExists, &viewExists, &receiptExists); err != nil {
		t.Fatal(err)
	}

	if !stateExists || !viewExists || receiptExists {
		t.Fatalf("rollback state=%t view=%t receipt=%t, want true,true,false", stateExists, viewExists, receiptExists)
	}
}

func TestLedgerBackfillStateDropRecordsReceiptWithDrop(t *testing.T) {
	var stateExists, receiptExists bool

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	pool := NewPool(t)
	ctx := t.Context()

	createProductionMigrationLedgerFixture(t, pool)
	createRetiredLedgerStateFixture(t, pool)
	if _, err := pool.Exec(ctx, strings.Replace(insertRetiredLedgerState, "%s", "now()", 1)); err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(ctx, pool, dir, ledgerBackfillStateDropMigration); err != nil {
		t.Fatalf("apply 229 with production ledger: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT to_regclass('youtube_notification_delivery_ledger_state') IS NOT NULL,
		EXISTS (SELECT 1 FROM public.schema_migrations WHERE filename = $1)`, ledgerBackfillStateDropMigration).
		Scan(&stateExists, &receiptExists); err != nil {
		t.Fatal(err)
	}

	if stateExists || !receiptExists {
		t.Fatalf("state=%t receipt=%t, want false,true", stateExists, receiptExists)
	}
	if err := applyMigrationFile(ctx, pool, dir, ledgerBackfillStateDropMigration); err != nil {
		t.Fatalf("reapply recorded 229: %v", err)
	}
}
