SELECT id, channel_id, image_url, COALESCE(image_sha256, ''), posted_at, revision
FROM schedule_posters
WHERE review_status = 'pending_review' AND review_requested_at IS NULL
ORDER BY posted_at DESC, id DESC
LIMIT $1
