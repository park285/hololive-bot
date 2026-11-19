SELECT id
FROM schedule_posters
WHERE channel_id = $1 AND review_status = 'confirmed' AND rows_extracted_at IS NOT NULL
  AND week_start + rows_day_shift <= $2::date + 7 AND week_end + rows_day_shift >= $2::date
ORDER BY week_start + rows_day_shift > $2::date,
         CASE WHEN week_start + rows_day_shift <= $2::date THEN week_start END DESC,
         week_start, posted_at DESC, id DESC
LIMIT 1
