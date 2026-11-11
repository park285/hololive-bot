WITH target AS (
    SELECT revision, review_status,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'confirmed', week_start = $3::date, week_end = $4::date, reviewed_at = now(),
        translation_status = 'pending', translation_error = '', revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'pending_review'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT review_status FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
