SELECT subject_key, observation_kind, reason_kind, reason_key
FROM youtube_collection_target_reasons
WHERE projection_generation = $1
ORDER BY subject_key, observation_kind, reason_kind, reason_key
