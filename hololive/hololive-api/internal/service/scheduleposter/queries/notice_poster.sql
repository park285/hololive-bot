SELECT id
FROM schedule_posters
WHERE id = $5 AND channel_id = $1 AND review_status = 'confirmed' AND rows_extracted_at IS NOT NULL
  AND week_end + rows_day_shift >= $2::date
