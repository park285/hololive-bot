SELECT (SELECT count(id) FROM source_observations),
       (SELECT count(observation_id) FROM source_observation_queue),
       (SELECT count(subject_key) FROM source_collection_checkpoints),
       (SELECT count(id) FROM source_observation_collisions)
