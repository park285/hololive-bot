SELECT id, template_id, body, created_at
		 FROM notification_template_revisions
		 WHERE template_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2