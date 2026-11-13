SELECT id, channel_id, image_url, COALESCE(image_sha256, ''), revision,
       translation_inputs_sha256, source_transcript, COALESCE(translation_table, '')
FROM schedule_posters
WHERE translation_status = 'pending' AND review_status = 'confirmed'
ORDER BY reviewed_at, id
LIMIT $1
