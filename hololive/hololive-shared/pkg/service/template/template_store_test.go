package template

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func saveTestTemplate(t *testing.T, pool *pgxpool.Pool, key domain.TemplateKey, channelID *string, body string) {
	t.Helper()

	const upsertDefault = `INSERT INTO notification_templates (template_key, channel_id, body) VALUES ($1, NULL, $2)
		ON CONFLICT (template_key, format_variant) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now()`

	const upsertOverride = `INSERT INTO notification_templates (template_key, channel_id, body) VALUES ($1, $2, $3)
		ON CONFLICT (template_key, channel_id, format_variant) WHERE channel_id IS NOT NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now()`

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
