INSERT INTO source_observation_queue (
    observation_id,
    status,
    attempt_count,
    replay_count,
    available_at
)
VALUES ($1, 'PENDING', 0, $2, NOW())
ON CONFLICT (observation_id) DO UPDATE
SET status = 'PENDING',
    attempt_count = 0,
    replay_count = EXCLUDED.replay_count,
    available_at = NOW(),
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = NULL,
    last_error_code = NULL,
    last_error_detail = NULL,
    updated_at = NOW()
WHERE source_observation_queue.status IN ('PROCESSED', 'DEAD_LETTER')
RETURNING observation_id
