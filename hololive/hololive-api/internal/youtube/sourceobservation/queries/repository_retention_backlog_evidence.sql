-- 종류별 원본을 최대 $3개만 검사하고 첫 비보호 원본에서 보호 판정을 멈춘다.
-- 후보 수는 보호 판정 없이 별도로 센다. 한도를 채우고 모두 보호 중이면 known=false다.
WITH policies AS (
    SELECT policy.kind, policy.cutoff FROM unnest($1::text[], $2::timestamptz[]) AS policy(kind, cutoff)
), measured AS (
    SELECT policy.kind,
           min(eligible.received_at) AS oldest,
           sum(candidate_count.inspected) AS inspected
    FROM policies policy
    CROSS JOIN LATERAL (
        SELECT count(candidate.id) AS inspected
        FROM (
            SELECT observation.id
            FROM source_observations observation
            WHERE observation.observation_kind=policy.kind AND observation.received_at<policy.cutoff
            ORDER BY observation.received_at, observation.id
            LIMIT $3
        ) candidate
    ) candidate_count
    LEFT JOIN LATERAL (
        SELECT candidate.received_at
        FROM (
            SELECT observation.id, observation.received_at
            FROM source_observations observation
            WHERE observation.observation_kind=policy.kind AND observation.received_at<policy.cutoff
            ORDER BY observation.received_at, observation.id
            LIMIT $3
        ) candidate
        WHERE NOT public.source_observation_retention_protected(candidate.id)
        ORDER BY candidate.received_at, candidate.id
        LIMIT 1
    ) eligible ON true
    GROUP BY policy.kind
)
SELECT min(oldest), bool_and(oldest IS NOT NULL OR inspected<$3) AS known
FROM measured
