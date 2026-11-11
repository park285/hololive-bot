WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET translation_status = 'pending', translation_error = '', translated_image = NULL, translated_mime = NULL,
        translated_at = NULL, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed' AND translation_status IN ('succeeded', 'failed')
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
