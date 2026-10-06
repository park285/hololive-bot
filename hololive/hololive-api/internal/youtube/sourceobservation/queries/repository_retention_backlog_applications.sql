-- 종류·적용 시각 순의 orphan 전용 인덱스에서 삭제 가능한 첫 행만 읽는다.
WITH policies AS (
    SELECT policy.kind, policy.cutoff FROM unnest($1::text[], $2::timestamptz[]) AS policy(kind, cutoff)
)
SELECT min(candidate.applied_at), true AS known
FROM policies policy
LEFT JOIN LATERAL (
    SELECT application.applied_at
    FROM source_observation_applications application
    WHERE application.observation_kind=policy.kind
      AND application.observation_id IS NULL
      AND application.applied_at<policy.cutoff
    ORDER BY application.applied_at, application.id
    LIMIT $3
) candidate ON true
