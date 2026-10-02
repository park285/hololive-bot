UPDATE source_observation_replay_requests
SET status = 'REJECTED',
    applied_at = NULL,
    rejection_code = $2
WHERE id = $1
  AND status = 'PENDING'
