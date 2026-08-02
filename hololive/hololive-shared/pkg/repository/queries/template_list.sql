SELECT id, template_key, channel_id, body, created_at, updated_at
FROM notification_templates
ORDER BY template_key, channel_id
