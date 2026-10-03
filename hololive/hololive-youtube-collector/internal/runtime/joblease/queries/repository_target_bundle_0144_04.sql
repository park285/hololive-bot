-- acquire 전용이다. cadence bundle의 주기·신규 입장 가능 여부와 lease에 기록할 membership 범위를 함께 계산한다.
-- not_before는 신규 입장에만 쓰며, 이미 획득한 작업의 snapshot·renew·complete·publish는 이 문장을 쓰지 않는다.
WITH cadence AS (
    SELECT COUNT(subject_key) AS target_count,
           COALESCE(MIN(poll_interval_ms), 0) AS min_interval_ms,
           COALESCE(MAX(poll_interval_ms), 0) AS max_interval_ms,
           COUNT(subject_key) FILTER (
               WHERE not_before IS NULL OR not_before <= statement_timestamp()
           ) AS admissible_count
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($2::text[])
      AND enabled = TRUE
      AND valid_until > statement_timestamp()
      AND (NOT $3::boolean OR subject_key = $4)
), membership AS (
    SELECT COUNT(subject_key) AS member_count,
           COALESCE(
               bool_and(COALESCE(member_since_generation BETWEEN 1 AND $1, FALSE)),
               FALSE
           ) AS members_proven
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($5::text[])
      AND enabled = TRUE
      AND valid_until > statement_timestamp()
      AND (NOT $3::boolean OR subject_key = $4)
)
SELECT cadence.target_count,
       cadence.min_interval_ms,
       cadence.max_interval_ms,
       cadence.admissible_count,
       membership.member_count,
       membership.members_proven
FROM cadence
CROSS JOIN membership
