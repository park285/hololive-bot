-- 확정본의 주 범위를 바꾸고 판정 주체를 관리자로 기록한다. 주 범위는 번역 입력이 아니라 번역본을 그대로 둔다.
-- review_requested_at은 summary의 최신 후보를 바꾸므로 건드리지 않으며, 그래서 같은 주 판정(week_coverage)은 자동 확정본으로 계속 본다.
-- 번역 대기 행은 바꾸지 않는다. revision이 오르면 진행 중인 번역의 결과 기록(revision 조건)이 버려지기 때문이다.
WITH target AS (
    SELECT revision, review_status = 'confirmed' AND translation_status <> 'pending' AS changeable,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET week_start = $3::date, week_end = $4::date, reviewed_at = now(), reviewed_by = 'admin', revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed' AND translation_status <> 'pending'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT changeable FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
