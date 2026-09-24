package dbtest

import "testing"

func TestTemplateReadabilityMigrationPreservesCustomBodiesAndIsIdempotent(t *testing.T) {
	pool := NewPool(t)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	// 채널별 본문은 이전 표준 본문이어도 전역 기본값 갱신 대상이 아닙니다.
	_, err = pool.Exec(t.Context(), `
INSERT INTO notification_templates(template_key, channel_id, body)
VALUES ('X_SPACE_STARTED', 'readability-test-channel', $template$## 🔴 **{{mdsafe .MemberName}}** 스페이스 시작
{{- if .Title}}
[{{mdsafe .Title}}]({{.URL}})
{{- else}}
{{.URL}}
{{- end}}$template$);
UPDATE notification_templates SET body = '운영자 지정 {{.Count}}'
WHERE template_key = 'CMD_LIVE_STREAMS' AND channel_id IS NULL;`)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := func() string {
		t.Helper()

		var value string

		// xmin까지 비교하여 값이 같은 불필요한 UPDATE도 검출합니다.
		err := pool.QueryRow(t.Context(), `SELECT jsonb_agg(jsonb_build_array(id, channel_id, body, updated_at, xmin::text) ORDER BY id)::text FROM notification_templates`).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}

		return value
	}
	before := snapshot()

	for range 2 {
		if err := applyMigrationFile(t.Context(), pool, dir, "203_chat_template_readability.sql"); err != nil {
			t.Fatal(err)
		}

		if after := snapshot(); after != before {
			t.Fatal("replay changed customized defaults, overrides, or already migrated rows")
		}
	}
}
