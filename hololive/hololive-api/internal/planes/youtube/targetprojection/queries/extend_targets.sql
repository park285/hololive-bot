-- 같은 hash의 CURRENT row는 이미 입력과 같은 집합이다. validity heartbeat와 not_before만 반영한다.
-- membership은 migration 또는 새 generation에서만 부여하며, NULL을 heartbeat로 복구하지 않는다.
UPDATE youtube_collection_targets AS target
SET valid_until = $2,
    not_before = input.not_before
FROM unnest($3::text[], $4::text[], $5::timestamptz[])
     AS input(subject_key, observation_kind, not_before)
WHERE target.projection_generation = $1
  AND target.subject_key = input.subject_key
  AND target.observation_kind = input.observation_kind
  AND (target.valid_until, target.not_before)
      IS DISTINCT FROM ($2::timestamptz, input.not_before)
