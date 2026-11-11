SELECT id, image_url, revision
FROM schedule_posters
WHERE translation_status = 'pending' AND review_status = 'confirmed'
ORDER BY reviewed_at, id
LIMIT $1
