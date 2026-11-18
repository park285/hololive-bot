UPDATE schedule_posters
SET rows_status = 'failed', rows_error = $3
WHERE id = $1 AND rows_revision = $2 AND review_status = 'confirmed' AND rows_status = 'pending'
