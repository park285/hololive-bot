INSERT INTO source_observation_queue (observation_id)
VALUES ($1)
ON CONFLICT (observation_id) DO NOTHING
