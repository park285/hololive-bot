package dbtest

import (
	"testing"
)

func TestNewPool_AppliesProdMigrations(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	for _, table := range []string{"acl_settings", "acl_rooms", "alarms"} {
		var exists bool

		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = current_schema()
				  AND table_name = $1
			)`, table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}

		if !exists {
			t.Fatalf("table %s not present after migrations", table)
		}
	}
}

func TestApplyMigrations_IsIdempotent(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}
}

func TestNewPool_RestoresBaseSchema(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	for _, table := range []string{"members", "alarms", "youtube_notification_outbox"} {
		var oid *string

		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, table).Scan(&oid); err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}

		if oid == nil {
			t.Fatalf("base table %s not present after full-chain migrations", table)
		}
	}

	wantColumns := map[string][]string{
		"members": {"id", "slug", "channel_id", "english_name", "aliases", "photo", "org", "sync_source", "twitch_user_id"},
		"alarms":  {"id", "room_id", "user_id", "channel_id", "created_at", "alarm_types"},
	}

	for table, columns := range wantColumns {
		for _, column := range columns {
			var exists bool

			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = current_schema()
					  AND table_name = $1
					  AND column_name = $2
				)`, table, column,
			).Scan(&exists)
			if err != nil {
				t.Fatalf("check column %s.%s: %v", table, column, err)
			}

			if !exists {
				t.Fatalf("column %s.%s missing after full-chain migrations", table, column)
			}
		}
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO members (slug, english_name, status, is_graduated, org, sync_source)
		 VALUES ('smoke-test', 'Smoke Test', 'active', false, 'Hololive', 'manual')`,
	); err != nil {
		t.Fatalf("insert into members: %v", err)
	}

	var count int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE slug = 'smoke-test'`).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}

	if count != 1 {
		t.Fatalf("expected 1 smoke-test member, got %d", count)
	}
}

func TestNewPool_IsolatesPerCall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	poolA := NewPool(t)
	poolB := NewPool(t)

	var dbA, dbB string

	if err := poolA.QueryRow(ctx, "SELECT current_database()").Scan(&dbA); err != nil {
		t.Fatalf("current_database A: %v", err)
	}

	if err := poolB.QueryRow(ctx, "SELECT current_database()").Scan(&dbB); err != nil {
		t.Fatalf("current_database B: %v", err)
	}

	if dbA == dbB {
		t.Fatalf("expected isolated databases, both got %q", dbA)
	}

	if _, err := poolA.Exec(ctx, "INSERT INTO acl_settings (key, value) VALUES ('enabled', 'true')"); err != nil {
		t.Fatalf("insert into database A: %v", err)
	}

	var countB int

	if err := poolB.QueryRow(ctx, "SELECT count(*) FROM acl_settings").Scan(&countB); err != nil {
		t.Fatalf("count database B: %v", err)
	}

	if countB != 0 {
		t.Fatalf("database B should be empty, got %d rows", countB)
	}
}
