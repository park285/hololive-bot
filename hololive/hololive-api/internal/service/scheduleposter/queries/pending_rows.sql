SELECT id, channel_id, image_url, COALESCE(image_sha256, ''), week_start, week_end, rows_revision
FROM schedule_posters
WHERE review_status = 'confirmed' AND rows_status = 'pending' AND week_end + rows_day_shift >= $1::date
ORDER BY reviewed_at, id
LIMIT $2
