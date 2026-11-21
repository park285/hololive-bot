UPDATE event_grade_requests
SET published_at=CASE WHEN $3::text IS NULL THEN statement_timestamp() END, dispatch_rejection=$3::text
WHERE id=$1 AND outcome=$2 AND published_at IS NULL AND ($3::text IS NULL OR dispatch_rejection IS NULL)
