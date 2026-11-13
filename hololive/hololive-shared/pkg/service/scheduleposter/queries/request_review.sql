UPDATE schedule_posters
SET review_requested_at = now()
WHERE id = $1 AND review_status = 'pending_review' AND review_requested_at IS NULL
