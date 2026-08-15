SELECT subject_key, observation_kind, priority, poll_interval_ms, enabled
FROM youtube_collection_targets
WHERE projection_generation = $1
ORDER BY subject_key COLLATE "C", observation_kind COLLATE "C"
