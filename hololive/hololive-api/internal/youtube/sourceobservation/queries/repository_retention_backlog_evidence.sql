-- 종류별 원본을 최대 $3개만 읽고 삭제와 같은 보호 판정을 적용한다.
-- 한도 안에서 적격 원본을 찾지 못했고 뒤에 더 있을 수 있으면 known=false다.
WITH policies AS (
    SELECT policy.kind, policy.cutoff FROM unnest($1::text[], $2::timestamptz[]) AS policy(kind, cutoff)
), measured AS (
    SELECT policy.kind,
           min(candidate.received_at) FILTER (WHERE
               NOT public.source_observation_retention_protected(candidate.id)
           ) AS oldest,
           count(candidate.id) AS inspected
    FROM policies policy
    LEFT JOIN LATERAL (
        SELECT observation.id, observation.received_at
        FROM source_observations observation
        WHERE observation.observation_kind=policy.kind AND observation.received_at<policy.cutoff
        ORDER BY observation.received_at, observation.id
        LIMIT $3
    ) candidate ON true
    GROUP BY policy.kind
)
SELECT min(oldest), bool_and(oldest IS NOT NULL OR inspected<$3) AS known
FROM measured
