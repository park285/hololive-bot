WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'rejected', reviewed_at = now(), reviewed_by = $3,
        rows_status = 'pending', rows_error = '', rows_extracted_at = NULL, rows_day_shift = 0, rows_revision = rows_revision + 1, revision = revision + 1, updated_at = now(),
        image_data = NULL, image_mime = NULL
    WHERE id = $1 AND revision = $2 AND review_status IN ('pending_review', 'confirmed')
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
