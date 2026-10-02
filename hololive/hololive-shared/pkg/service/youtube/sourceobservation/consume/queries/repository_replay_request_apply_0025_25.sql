UPDATE source_observation_replay_requests
SET status = 'APPLIED',
    applied_at = NOW(),
    rejection_code = NULL
WHERE id = $1
  AND status = 'PENDING'
