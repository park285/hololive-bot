DELETE FROM source_observation_queue AS queue
WHERE queue.observation_id IN (
    SELECT candidate.observation_id
    FROM source_observation_queue AS candidate
    WHERE (
        ($1::timestamptz IS NOT NULL
            AND candidate.status = 'PROCESSED'
            AND candidate.processed_at < $1)
        OR
        ($2::timestamptz IS NOT NULL
            AND candidate.status = 'DEAD_LETTER'
            AND candidate.dead_lettered_at < $2)
    )
    ORDER BY candidate.updated_at, candidate.observation_id
    LIMIT $3
)
