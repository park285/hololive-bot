-- CURRENT 세대의 PK 범위를 한 번 읽어 입력과 비교한 뒤 변경 tuple만 찾아 갱신한다.
-- membership은 migration 또는 새 generation에서만 부여하며, NULL을 heartbeat로 복구하지 않는다.
WITH current_targets AS MATERIALIZED (
    SELECT ctid, subject_key, observation_kind, not_before
    FROM youtube_collection_targets
    WHERE projection_generation = $1
), changed AS MATERIALIZED (
    SELECT target.ctid, input.not_before
    FROM current_targets AS target
    JOIN unnest($2::text[], $3::text[], $4::timestamptz[])
         AS input(subject_key, observation_kind, not_before)
      ON target.subject_key = input.subject_key
     AND target.observation_kind = input.observation_kind
    WHERE target.not_before IS DISTINCT FROM input.not_before
)
UPDATE youtube_collection_targets AS target
SET not_before = changed.not_before
FROM changed
WHERE target.ctid = changed.ctid
