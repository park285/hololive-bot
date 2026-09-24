package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCopyableCommandMigrationPreservesOverridesAndReplay(t *testing.T) {
	for _, tc := range []struct{ file, key string }{
		{"206_copyable_command_prefix.sql", "CMD_HELP"},
		{"207_copyable_member_argument.sql", "CMD_AMBIGUOUS_MEMBER"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			testCopyableCommandMigration(t, tc.file, tc.key)
		})
	}
}

func testCopyableCommandMigration(t *testing.T, file, key string) {
	t.Helper()

	pool := NewPool(t)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	expected, previous := prepareCopyableCommandUpgrade(t, pool, file, key)

	if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct{ key, channel, body string }{
		{key, "", expected},
		{key, "copyable-prefix-override", previous},
	} {
		var got string

		if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key=$1 AND coalesce(channel_id,'')=$2`, check.key, check.channel).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != check.body {
			t.Errorf("migration changed wrong body: key=%s channel=%s", check.key, check.channel)
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
		if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
			t.Fatal(err)
		}

		if snapshot() != before {
			t.Fatal("replay changed default or customized rows")
		}
	}

	// 같은 키의 전역 사용자 지정 본문도 표준 본문으로 덮어쓰지 않습니다.
	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='custom {{.Prefix}}' WHERE template_key=$1 AND channel_id IS NULL`, key); err != nil {
		t.Fatal(err)
	}

	before = snapshot()

	if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
		t.Fatal(err)
	}

	if snapshot() != before {
		t.Fatal("migration replaced a customized default")
	}
}

func prepareCopyableCommandUpgrade(t *testing.T, pool *pgxpool.Pool, file, key string) (string, string) {
	t.Helper()

	body, ok := loadTemplateMigrationBodies(t, file)[key]
	if !ok {
		t.Fatalf("migration %s does not contain %s", file, key)
	}

	expected, previous := body.newBody, body.oldBody

	for _, statement := range []string{
		`UPDATE notification_templates SET body=$1 WHERE template_key=$2 AND channel_id IS NULL`,
		`INSERT INTO notification_templates(template_key,channel_id,body) VALUES ($2,'copyable-prefix-override',$1)`,
	} {
		if _, err := pool.Exec(t.Context(), statement, previous, key); err != nil {
			t.Fatal(err)
		}
	}

	return expected, previous
}
