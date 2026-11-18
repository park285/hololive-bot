WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET rows_status = 'pending', rows_error = '', rows_revision = rows_revision + 1
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed' AND rows_status IN ('succeeded', 'failed')
      AND week_end + rows_day_shift >= (now() AT TIME ZONE 'Asia/Tokyo')::date
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
