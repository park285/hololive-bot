INSERT INTO notification_templates (template_key, channel_id, body)
VALUES ($1, NULL, $2)
ON CONFLICT (template_key) WHERE channel_id IS NULL
DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
RETURNING id, template_key, channel_id, body, created_at, updated_at
