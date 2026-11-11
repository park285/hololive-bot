UPDATE schedule_posters
SET translation_status = 'failed', translation_error = $3, revision = revision + 1, updated_at = now()
WHERE id = $1 AND revision = $2 AND translation_status = 'pending' AND review_status = 'confirmed'
