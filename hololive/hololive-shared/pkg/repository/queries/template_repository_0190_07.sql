SELECT id, template_key, channel_id, body, created_at, updated_at
		 FROM notification_templates
		 WHERE template_key = $1 AND channel_id IS NOT NULL
		 ORDER BY channel_id