INSERT INTO notification_template_revisions (template_id, body, created_at)
VALUES ($1, $2, clock_timestamp())
