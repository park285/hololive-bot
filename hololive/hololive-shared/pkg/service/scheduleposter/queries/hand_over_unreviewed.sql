UPDATE schedule_posters
SET review_requested_at = now()
WHERE review_status = 'pending_review' AND review_requested_at IS NULL AND created_at < $1
