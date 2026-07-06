DELETE FROM notification_template_revisions
			 WHERE template_id = $1
			   AND id NOT IN (
			       SELECT id
			       FROM notification_template_revisions
			       WHERE template_id = $1
			       ORDER BY created_at DESC
			       LIMIT $2
			   )