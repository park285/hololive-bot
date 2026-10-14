-- 획득 전용 bundle이다. cadence와 membership을 같은 target 스캔에서 각각 집계한다.
-- not_before는 신규 입장에만 쓰며 이미 획득한 작업의 membership에는 적용하지 않는다.
WITH current_projection AS (
    SELECT generation
    FROM youtube_collection_projection_generations
    WHERE generation = $1 AND status = 'CURRENT'
      AND valid_until > statement_timestamp()
)
SELECT COUNT(*) FILTER (WHERE observation_kind = ANY($2::text[])) AS target_count,
       COALESCE(MIN(poll_interval_ms) FILTER (WHERE observation_kind = ANY($2::text[])), 0) AS min_interval_ms,
       COALESCE(MAX(poll_interval_ms) FILTER (WHERE observation_kind = ANY($2::text[])), 0) AS max_interval_ms,
       COUNT(*) FILTER (
           WHERE observation_kind = ANY($2::text[])
             AND (not_before IS NULL OR not_before <= statement_timestamp())
       ) AS admissible_count,
       COUNT(*) FILTER (WHERE observation_kind = ANY($3::text[])) AS member_count,
       COALESCE(bool_and(COALESCE(member_since_generation BETWEEN 1 AND $1, FALSE))
           FILTER (WHERE observation_kind = ANY($3::text[])), FALSE) AS members_proven,
       EXISTS (SELECT 1 FROM current_projection) AS current
FROM youtube_collection_targets
WHERE projection_generation = (SELECT generation FROM current_projection)
  AND observation_kind = ANY($2::text[] || $3::text[])
  AND enabled = TRUE
