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
