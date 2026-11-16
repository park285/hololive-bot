SELECT id, channel_id, review_status = 'confirmed', week_start, week_end, rows_extracted_at, posted_at, rows_status = 'pending',
       rows_day_shift
FROM schedule_posters
WHERE id = $1
