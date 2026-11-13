UPDATE schedule_posters
SET translation_status = 'failed', translation_error = $3, revision = revision + 1, updated_at = now(),
    translation_inputs_sha256 = $4, source_transcript = $5, translation_table = NULLIF($6::text, '')
WHERE id = $1 AND revision = $2 AND translation_status = 'pending' AND review_status = 'confirmed'
