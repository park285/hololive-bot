SELECT
    (SELECT count(*) FROM schedule_posters WHERE review_status = 'pending_review'),
    (SELECT id FROM schedule_posters ORDER BY created_at DESC, id DESC LIMIT 1)
