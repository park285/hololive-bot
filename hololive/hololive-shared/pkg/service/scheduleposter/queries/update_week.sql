-- 확정본의 주 범위만 바꾸고 관리자가 확인한 확정본으로 표시한다. 주 범위는 번역 입력이 아니라 번역본을 그대로 두고,
-- review_requested_at은 summary의 최신 후보를 바꾸므로 건드리지 않는다.
WITH target AS (
    SELECT revision, review_status,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET week_start = $3::date, week_end = $4::date, reviewed_by = 'admin', revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT review_status FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
