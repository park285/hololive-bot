-- 자동 확정본(관리자에게 넘긴 적 없는 확정본)은 틀렸을 수 있고, 같은 출처에서 확정본보다 늦게 올린 후보는 수정본일 수 있어
-- 둘 다 같은 주로 치지 않는다. 같은 주로 친 후보는 expired로 닫혀 되돌릴 수 없다.
WITH candidate AS (
    SELECT channel_id, source, posted_at FROM schedule_posters WHERE id = $1
)
SELECT COALESCE(bool_or(confirmed.week_start = $2::date AND confirmed.week_end = $3::date
                        AND confirmed.review_requested_at IS NOT NULL
                        AND NOT (confirmed.source = candidate.source AND confirmed.posted_at < candidate.posted_at)), false),
       count(confirmed.id) > 0
FROM candidate
JOIN schedule_posters AS confirmed ON confirmed.channel_id = candidate.channel_id
WHERE confirmed.review_status = 'confirmed' AND confirmed.week_start <= $3::date AND confirmed.week_end >= $2::date
