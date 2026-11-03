WITH locked AS MATERIALIZED (
    SELECT observation_id, status, lease_token, attempt_count, lease_expires_at
    FROM source_observation_queue
    WHERE observation_id = $1
    FOR UPDATE
), timed AS MATERIALIZED (
    SELECT observation_id, status, lease_token, attempt_count, lease_expires_at,
           clock_timestamp() AS transitioned_at
    FROM locked
)
UPDATE source_observation_queue AS queue
SET status = 'PENDING',
    available_at = timed.transitioned_at + ($3::bigint * INTERVAL '1 millisecond'),
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = NULL,
    last_error_code = $4,
    last_error_detail = NULLIF($5, ''),
    updated_at = timed.transitioned_at
FROM timed
WHERE queue.observation_id = timed.observation_id
  AND timed.status = 'PROCESSING'
  AND timed.lease_token = $2
  AND timed.attempt_count = $6
  AND timed.lease_expires_at > timed.transitioned_at
RETURNING queue.observation_id
