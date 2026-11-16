-- 검토 대기 목록은 summary와 같이 관리자 검토로 넘긴 후보만 보여 준다.
SELECT id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt, review_status,
       week_start, week_end, reviewed_at,
       revision, created_at, updated_at, COALESCE(reviewed_by, ''), COALESCE(review_request_reason, ''),
       rows_status, rows_error, rows_extracted_at,
       (SELECT count(*) FROM schedule_poster_rows r WHERE r.poster_id = schedule_posters.id)
FROM schedule_posters
WHERE review_status = $1 AND (review_status <> 'pending_review' OR review_requested_at IS NOT NULL)
ORDER BY created_at DESC, id DESC
LIMIT $2
