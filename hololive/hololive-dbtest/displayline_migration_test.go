package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDisplayLineMigrationUpgradePreservesCustomBodiesAndReplay(t *testing.T) {
	pool := NewPool(t)

	const file = "207_template_displayline.sql"

	bodies := loadTemplateMigrationBodies(t, file)

	if len(bodies) != 36 {
		t.Fatalf("expected 36 template updates, got %d", len(bodies))
	}

	for key, body := range bodies {
		if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key=$2 AND channel_id IS NULL`, body.oldBody, key); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body) VALUES ('CMD_PROFILE','displayline-override',$1)`, bodies["CMD_PROFILE"].oldBody); err != nil {
		t.Fatal(err)
	}

	const custom = "사용자 지정 {{.Count}}"

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key='CMD_LIVE_STREAMS' AND channel_id IS NULL`, custom); err != nil {
		t.Fatal(err)
	}

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
		t.Fatal(err)
	}

	assertDisplayLineUpgrade(t, pool, bodies, custom)

	snapshot := func() string {
		t.Helper()

		var value string

		if err := pool.QueryRow(t.Context(), `SELECT jsonb_agg(jsonb_build_array(id,body,updated_at,xmin::text) ORDER BY id)::text FROM notification_templates`).Scan(&value); err != nil {
			t.Fatal(err)
		}

		return value
	}
	before := snapshot()

	for range 2 {
		if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
			t.Fatal(err)
		}

		if snapshot() != before {
			t.Fatal("replay rewrote templates")
		}
	}
}

func assertDisplayLineUpgrade(t *testing.T, pool *pgxpool.Pool, bodies map[string]templateMigrationBody, custom string) {
	t.Helper()

	for key, body := range bodies {
		want := body.newBody

		if key == "CMD_LIVE_STREAMS" {
			want = custom
		}

		var got string

		if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key=$1 AND channel_id IS NULL`, key).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != want {
			t.Errorf("upgrade changed wrong body: %s", key)
		}
	}

	var override string

	if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key='CMD_PROFILE' AND channel_id='displayline-override'`).Scan(&override); err != nil {
		t.Fatal(err)
	}

	if override != bodies["CMD_PROFILE"].oldBody {
		t.Fatal("channel override changed")
	}
}
