INSERT INTO source_observation_replay_requests (
    observation_id,
    provider,
    observation_kind,
    subject_key,
    observation_key,
    evidence_sha256,
    requested_by,
    reason,
    previous_attempt_count
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id
