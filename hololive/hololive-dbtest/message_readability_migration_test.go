package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMessageReadabilityMigrationUpgradePreservesOverridesAndReplay(t *testing.T) {
	pool := NewPool(t)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	expectedBody, previousBody := prepareReadabilityUpgrade(t, pool)

	if err := applyMigrationFile(t.Context(), pool, dir, "205_message_readability_completion.sql"); err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct{ key, channel, body string }{
		{"X_SPACE_STARTED", "", expectedBody},
		{"X_SPACE_STARTED", "readability-override", previousBody},
		{"CMD_LIVE_STREAMS", "", "사용자 지정 {{.Count}}"},
	} {
		var got string

		if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key=$1 AND coalesce(channel_id,'')=$2`, check.key, check.channel).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != check.body {
			t.Fatalf("upgrade/preservation mismatch: key=%s channel=%s", check.key, check.channel)
		}
	}

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
		if err := applyMigrationFile(t.Context(), pool, dir, "205_message_readability_completion.sql"); err != nil {
			t.Fatal(err)
		}

		if snapshot() != before {
			t.Fatal("migration replay rewrote already updated or customized rows")
		}
	}
}

func prepareReadabilityUpgrade(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()

	body, ok := loadTemplateMigrationBodies(t, "205_message_readability_completion.sql")["X_SPACE_STARTED"]
	if !ok {
		t.Fatal("204 migration is missing X_SPACE_STARTED")
	}

	expectedBody, previousBody := body.newBody, body.oldBody

	_, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key='X_SPACE_STARTED' AND channel_id IS NULL`, previousBody)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body) VALUES ('X_SPACE_STARTED','readability-override',$1)`, previousBody)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `UPDATE notification_templates SET body='사용자 지정 {{.Count}}' WHERE template_key='CMD_LIVE_STREAMS' AND channel_id IS NULL`)
	if err != nil {
		t.Fatal(err)
	}

	return expectedBody, previousBody
}
