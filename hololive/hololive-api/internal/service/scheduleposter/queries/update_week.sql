WITH target AS (
    SELECT revision, review_status = 'confirmed' AS changeable,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET week_start = $3::date, week_end = $4::date, reviewed_at = now(), reviewed_by = 'admin',
        rows_status = 'pending', rows_error = '', rows_extracted_at = NULL, rows_day_shift = 0, rows_revision = rows_revision + 1, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT changeable FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
