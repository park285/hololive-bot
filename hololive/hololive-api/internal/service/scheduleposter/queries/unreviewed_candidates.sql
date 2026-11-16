-- 같은 주의 원본과 수정본이 함께 들어오면 늦게 올린 것을 먼저 확정하도록 최근 게시물부터 고른다.
SELECT id, channel_id, image_url, COALESCE(image_sha256, ''), posted_at, revision
FROM schedule_posters
WHERE review_status = 'pending_review' AND review_requested_at IS NULL
ORDER BY posted_at DESC, id DESC
LIMIT $1
