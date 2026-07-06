UPDATE notification_templates
		 SET body = $1, updated_at = NOW()
		 WHERE id = $2
		 RETURNING id, template_key, channel_id, body, created_at, updated_at