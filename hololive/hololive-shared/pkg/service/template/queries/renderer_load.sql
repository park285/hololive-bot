SELECT body, row_version
FROM notification_templates
WHERE id = $1 AND template_key = $2
  AND channel_id IS NOT DISTINCT FROM NULLIF($3, '')
