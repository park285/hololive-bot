-- $7은 직전 CURRENT generation(없으면 NULL)이다. scheduling identity가 같은 row만 membership 시작
-- generation과 created_at(논리적 membership 시작 시각)을 함께 이어받는다. 직전 값이 NULL이거나
-- 새·변경·재등록 row이면 이 generation($1)과 현재 transaction 시각에서 시작한다. 무관한 generation
-- 교체가 미획득 target의 due 기준 시각을 되돌리지 않게 한다.
INSERT INTO youtube_collection_targets (
    projection_generation, subject_key, observation_kind,
    priority, poll_interval_ms, enabled,
    member_since_generation, created_at, not_before
)
SELECT $1, input.subject_key, input.observation_kind,
       input.priority, input.poll_interval_ms, input.enabled,
       COALESCE(previous.member_since_generation, $1),
       COALESCE(CASE WHEN previous.member_since_generation IS NOT NULL THEN previous.created_at END, now()),
       input.not_before
FROM unnest($2::text[], $3::text[], $4::smallint[], $5::bigint[], $6::boolean[], $8::timestamptz[])
     AS input(subject_key, observation_kind, priority, poll_interval_ms, enabled, not_before)
LEFT JOIN youtube_collection_targets AS previous
  ON previous.projection_generation = $7::bigint
 AND previous.subject_key = input.subject_key
 AND previous.observation_kind = input.observation_kind
 AND previous.priority = input.priority
 AND previous.poll_interval_ms = input.poll_interval_ms
 AND previous.enabled = input.enabled
