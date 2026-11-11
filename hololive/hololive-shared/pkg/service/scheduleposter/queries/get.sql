SELECT id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt, review_status,
       week_start, week_end, reviewed_at, translation_status, translation_error, translated_at,
       revision, created_at, updated_at
FROM schedule_posters
WHERE id = $1
