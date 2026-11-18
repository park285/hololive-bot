UPDATE schedule_posters
SET rows_status = 'succeeded', rows_error = '', rows_extracted_at = now(), rows_day_shift = $3
WHERE id = $1 AND rows_revision = $2 AND review_status = 'confirmed' AND rows_status = 'pending'
