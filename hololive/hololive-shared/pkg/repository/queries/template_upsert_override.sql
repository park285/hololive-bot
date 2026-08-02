INSERT INTO notification_templates (template_key, channel_id, body)
VALUES ($1, $2, $3)
ON CONFLICT (template_key, channel_id) WHERE channel_id IS NOT NULL
DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
RETURNING id, template_key, channel_id, body, created_at, updated_at
