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
