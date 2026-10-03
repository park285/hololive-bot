-- 기존 활성 후보 scan 순서를 먼저 고정하여 동률 후보·fallback 출처 순서를 보존한다.
-- 기간 밖 non-finite 날짜와 NULL 멤버 원소도 반환하여 기존 scan 오류를 숨기지 않는다.
-- window의 ordinal을 먼저 계산하고 선택 뒤 정렬하여 넓은 후보 전체의 materialization을 피한다.
WITH active_candidates AS (
    SELECT id, type, title, description, members, pub_date, event_start_date, link,
           row_number() OVER () AS candidate_ordinal
    FROM major_events
    WHERE status = 'active'
      AND type IN ('news', 'event')
      AND COALESCE(link_status, 'unchecked') NOT IN ('failed', 'blocked')
), period_bounds AS (
    -- pgx DATE는 UTC 자정이다. UTC 경계의 ceil DATE와 비교하여 DATE의 넓은 유한 범위를 보존한다.
    SELECT
        ($1::timestamptz AT TIME ZONE 'UTC')::date
            + CASE WHEN ($1::timestamptz AT TIME ZONE 'UTC')::time = time '00:00:00' THEN 0 ELSE 1 END AS date_start,
        ($2::timestamptz AT TIME ZONE 'UTC')::date
            + CASE WHEN ($2::timestamptz AT TIME ZONE 'UTC')::time = time '00:00:00' THEN 0 ELSE 1 END AS date_end
)
SELECT candidate.id, candidate.type, COALESCE(candidate.title, ''), COALESCE(candidate.description, ''),
       COALESCE(candidate.members, '{}'::text[]), candidate.pub_date, candidate.event_start_date, COALESCE(candidate.link, '')
FROM active_candidates candidate
CROSS JOIN period_bounds bounds
WHERE NOT isfinite(candidate.pub_date)
   OR NOT isfinite(candidate.event_start_date)
   OR EXISTS (SELECT 1 FROM unnest(candidate.members) member_name WHERE member_name IS NULL)
   OR CASE WHEN (candidate.type = 'news' AND candidate.pub_date IS NOT NULL)
                     OR (candidate.type = 'event' AND candidate.event_start_date IS NULL)
           THEN candidate.pub_date >= $1::timestamptz AND candidate.pub_date < $2::timestamptz
           ELSE candidate.event_start_date >= bounds.date_start AND candidate.event_start_date < bounds.date_end
      END
ORDER BY candidate_ordinal
