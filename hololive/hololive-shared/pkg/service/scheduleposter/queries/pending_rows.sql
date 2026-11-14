-- 주가 끝난 확정본은 !스케쥴로 보이지 않으므로 뽑지 않는다. $1은 JST 오늘 날짜다.
SELECT id, channel_id, image_url, COALESCE(image_sha256, ''), week_start, week_end, rows_revision
FROM schedule_posters
WHERE review_status = 'confirmed' AND rows_status = 'pending' AND week_end >= $1::date
ORDER BY reviewed_at, id
LIMIT $2
