WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'expired', revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'pending_review'
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
