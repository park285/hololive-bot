SELECT translated_mime, translated_image
FROM schedule_posters
WHERE channel_id = $1 AND review_status = 'confirmed' AND translation_status = 'succeeded'
  AND week_start <= $2::date AND week_end >= $2::date
ORDER BY week_start DESC, id DESC
LIMIT 1
