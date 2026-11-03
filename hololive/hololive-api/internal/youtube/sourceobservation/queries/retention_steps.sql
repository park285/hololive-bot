DELETE FROM source_observation_collisions
WHERE id IN (
    SELECT candidate.id
    FROM source_observation_collisions AS candidate
    WHERE candidate.occurred_at < $1
    ORDER BY candidate.occurred_at, candidate.id
    LIMIT $2
)
