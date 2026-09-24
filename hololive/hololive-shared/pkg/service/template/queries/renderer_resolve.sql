SELECT id, row_version, COALESCE(channel_id, '')
FROM notification_templates
WHERE template_key = $1 AND (channel_id = NULLIF($2, '') OR channel_id IS NULL)
ORDER BY channel_id IS NULL
LIMIT 1
