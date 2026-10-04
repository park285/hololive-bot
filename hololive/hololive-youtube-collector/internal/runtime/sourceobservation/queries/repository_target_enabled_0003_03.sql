-- 각 관측의 (subject, kind)가 현재 CURRENT에서 활성이고, lease 획득 generation 이전부터 같은 의미로 이어진 대상인지 확인한다.
-- RETIRED target의 valid_until이 남아 있어도 허용하지 않는다.
WITH current_projection AS (
    SELECT generation
    FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT'
      AND valid_until > statement_timestamp()
), requested AS (
    SELECT input.subject_key,
           input.observation_kind
    FROM unnest($2::text[], $3::text[]) AS input(subject_key, observation_kind)
)
SELECT EXISTS (SELECT 1 FROM current_projection)
   AND NOT EXISTS (
    SELECT 1
    FROM requested
    WHERE NOT EXISTS (
        SELECT 1
        FROM current_projection
        JOIN youtube_collection_targets AS target
          ON target.projection_generation = current_projection.generation
        WHERE target.subject_key = requested.subject_key
          AND target.observation_kind = requested.observation_kind
          AND target.enabled = TRUE
          AND target.valid_until > statement_timestamp()
          AND target.member_since_generation BETWEEN 1 AND $1::bigint
    )
)
