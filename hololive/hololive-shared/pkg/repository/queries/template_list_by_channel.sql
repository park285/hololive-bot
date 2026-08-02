SELECT id, template_key, channel_id, body, created_at, updated_at
FROM notification_templates
WHERE channel_id = $1
ORDER BY template_key, channel_id
