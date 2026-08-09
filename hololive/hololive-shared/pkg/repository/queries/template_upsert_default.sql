INSERT INTO notification_templates (template_key, channel_id, body)
VALUES ($1, NULL, $2)
ON CONFLICT (template_key) WHERE channel_id IS NULL
DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
RETURNING
    NEW.id,
    NEW.template_key,
    NEW.channel_id,
    NEW.body,
    NEW.created_at,
    NEW.updated_at,
    OLD.body AS previous_body
