UPDATE schedule_posters
SET translation_status = 'succeeded', translated_image = $3, translated_mime = $4, translated_at = now(),
    translation_error = '', revision = revision + 1, updated_at = now(),
    translation_inputs_sha256 = NULL, source_transcript = NULL, translation_table = NULL
WHERE id = $1 AND revision = $2 AND translation_status = 'pending' AND review_status = 'confirmed'
