INSERT INTO source_observation_replay_epoch (
    singleton,
    cutoff_received_at,
    activated_by,
    reason
)
VALUES (true, clock_timestamp(), $1, $2)
ON CONFLICT (singleton) DO SELECT
RETURNING cutoff_received_at, activated_by, reason,
          OLD.singleton IS NULL AS activated
