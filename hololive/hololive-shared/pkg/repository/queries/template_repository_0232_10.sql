SELECT id, template_id, body, created_at
		 FROM notification_template_revisions
		 WHERE id = $1