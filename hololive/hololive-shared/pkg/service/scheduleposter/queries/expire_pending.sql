UPDATE schedule_posters
SET review_status = 'expired', revision = revision + 1, updated_at = now()
WHERE review_status = 'pending_review' AND created_at < $1
