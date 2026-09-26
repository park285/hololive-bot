package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	compactSeparatorFile     = "217_template_compact_separators.sql"
	compactSeparatorOverride = "compact-override"
	compactSeparatorCustom   = "사용자 지정 {{.Count}}"
	compactSeparatorKeptKey  = "CMD_UPCOMING_STREAMS"
)

func TestCompactSeparatorMigrationUpgradePreservesCustomBodiesAndReplay(t *testing.T) {
	pool := NewPool(t)
	bodies := loadTemplateMigrationBodies(t, compactSeparatorFile)

	if len(bodies) != 15 {
		t.Fatalf("expected 15 template updates, got %d", len(bodies))
	}

	seedCompactSeparatorUpgrade(t, pool, bodies)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(t.Context(), pool, dir, compactSeparatorFile); err != nil {
		t.Fatal(err)
	}

	assertCompactSeparatorUpgrade(t, pool, bodies)
	assertTemplateReplayStable(t, pool, dir, compactSeparatorFile)
}

// 운영 DB처럼 208 표준 본문, 채널 override, 운영자가 바꾼 전역 본문이 섞인 상태를 만든다.
func seedCompactSeparatorUpgrade(t *testing.T, pool *pgxpool.Pool, bodies map[string]templateMigrationBody) {
	t.Helper()

	for key, body := range bodies {
		if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key=$2 AND channel_id IS NULL`, body.oldBody, key); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body) VALUES ('CMD_ALARM_LIST',$1,$2)`,
		compactSeparatorOverride, bodies["CMD_ALARM_LIST"].oldBody); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key=$2 AND channel_id IS NULL`,
		compactSeparatorCustom, compactSeparatorKeptKey); err != nil {
		t.Fatal(err)
	}
}

func assertCompactSeparatorUpgrade(t *testing.T, pool *pgxpool.Pool, bodies map[string]templateMigrationBody) {
	t.Helper()

	for key, body := range bodies {
		want := body.newBody

		if key == compactSeparatorKeptKey {
			want = compactSeparatorCustom
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

	if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key='CMD_ALARM_LIST' AND channel_id=$1`,
		compactSeparatorOverride).Scan(&override); err != nil {
		t.Fatal(err)
	}

	if override != bodies["CMD_ALARM_LIST"].oldBody {
		t.Fatal("channel override changed")
	}
}

func assertTemplateReplayStable(t *testing.T, pool *pgxpool.Pool, dir, file string) {
	t.Helper()

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
