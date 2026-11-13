SELECT
    (SELECT count(*) FROM schedule_posters WHERE review_status = 'pending_review' AND review_requested_at IS NOT NULL),
    (SELECT id FROM schedule_posters WHERE review_requested_at IS NOT NULL ORDER BY review_requested_at DESC, id DESC LIMIT 1)
