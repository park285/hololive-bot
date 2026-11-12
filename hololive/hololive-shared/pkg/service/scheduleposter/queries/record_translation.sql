UPDATE schedule_posters
SET translation_status = 'succeeded', translated_image = $3, translated_mime = $4, translated_at = now(),
    translation_error = '', revision = revision + 1, updated_at = now()
WHERE id = $1 AND revision = $2 AND translation_status = 'pending' AND review_status = 'confirmed'
