package dbtest

import (
	"strings"
	"testing"
)

const ledgerBackfillClosedMigration = "227_youtube_delivery_ledger_backfill_closed.sql"

// migration 227은 alarm-worker의 ledger 완료 gate(ensureReady)를 대신해 적용 시점에 backfill 완료를 확인한다
// (DEC-20260926-hololive-retired-rollback-tooling). 완료되지 않은 state나 backfill 없이 190을 지난 DB는 거절하고,
// 빈 DB와 완료된 운영 DB는 통과해야 한다.
func TestLedgerBackfillClosedMigrationGuardsCompletion(t *testing.T) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	const insertState = `
INSERT INTO youtube_notification_delivery_ledger_state (
    singleton, schema_version, delivery_high_water_id, outbox_high_water_id,
    delivery_cursor_id, delivery_verify_cursor_id, outbox_cursor_id,
    legacy_coverage_start_at, coverage_verified_at, started_at, completed_at, updated_at
) VALUES (true, 1, 0, 0, 0, 0, 0, now(), now(), now(), %s, now())`

	cases := []struct {
		name    string
		setup   string
		wantErr string
	}{
		{name: "empty database", setup: ""},
		{name: "completed state", setup: strings.Replace(insertState, "%s", "now()", 1)},
		{
			name:    "incomplete state",
			setup:   strings.Replace(insertState, "%s", "NULL", 1),
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
