SELECT review_requested_at IS NULL FROM schedule_posters WHERE id = $1 FOR UPDATE
