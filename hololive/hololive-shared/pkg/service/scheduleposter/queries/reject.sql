WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'rejected', reviewed_at = now(), translation_status = 'not_requested', translation_error = '',
        translated_image = NULL, translated_mime = NULL, translated_at = NULL, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status IN ('pending_review', 'confirmed')
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
