INSERT INTO notification_templates(template_key, channel_id, body)
		 VALUES ($1, $2, $3)
		 RETURNING id, template_key, channel_id, body, created_at, updated_at