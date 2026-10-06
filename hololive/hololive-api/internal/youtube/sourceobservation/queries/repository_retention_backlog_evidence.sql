-- 종류별 오래된 원본을 최대 $3개만 읽고 실제 삭제의 큐·replay·종료 근거 보호를 적용한다.
-- 한도 안에서 적격 원본을 찾지 못했고 뒤에 더 있을 수 있으면 known=false다.
WITH policies AS (
    SELECT policy.kind, policy.cutoff FROM unnest($1::text[], $2::timestamptz[]) AS policy(kind, cutoff)
), measured AS (
    SELECT policy.kind,
           min(candidate.received_at) FILTER (WHERE
               queue.observation_id IS NULL
               AND NOT EXISTS (
                   SELECT 1 FROM source_observation_replay_requests replay
                   WHERE replay.observation_id=candidate.id AND replay.status='PENDING')
               AND NOT EXISTS (
                   SELECT 1 FROM youtube_live_reconciliation_heads head
                   WHERE head.end_candidate_observation_id=candidate.id)
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
    LEFT JOIN source_observation_queue queue ON queue.observation_id=candidate.id
    GROUP BY policy.kind
)
SELECT min(oldest), bool_and(oldest IS NOT NULL OR inspected<$3) AS known
FROM measured
