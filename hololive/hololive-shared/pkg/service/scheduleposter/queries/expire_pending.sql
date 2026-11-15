UPDATE schedule_posters
SET review_status = 'expired', revision = revision + 1, updated_at = now(), image_data = NULL, image_mime = NULL
WHERE review_status = 'pending_review' AND created_at < $1
