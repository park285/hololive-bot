package template

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// saveTestTemplate는 운영의 본문 변경(migration upsert)과 같은 SQL로 기본·override 본문을 바꾼다.
// row_version이 바뀌므로 Renderer는 캐시 무효화 호출 없이 다음 렌더부터 새 본문을 써야 한다.
func saveTestTemplate(t *testing.T, pool *pgxpool.Pool, key domain.TemplateKey, channelID *string, body string) {
	t.Helper()

	const upsertDefault = `INSERT INTO notification_templates (template_key, channel_id, body) VALUES ($1, NULL, $2)
		ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now()`

	const upsertOverride = `INSERT INTO notification_templates (template_key, channel_id, body) VALUES ($1, $2, $3)
		ON CONFLICT (template_key, channel_id) WHERE channel_id IS NOT NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now()`

	var err error
	if channelID == nil {
		_, err = pool.Exec(t.Context(), upsertDefault, key, body)
	} else {
		_, err = pool.Exec(t.Context(), upsertOverride, key, *channelID, body)
	}

	if err != nil {
		t.Fatalf("save template %s: %v", key, err)
	}
}
