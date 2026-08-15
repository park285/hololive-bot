SELECT subject_key
FROM youtube_collection_targets
WHERE projection_generation = $1
  AND observation_kind = $2
  AND enabled = TRUE
  AND valid_until > clock_timestamp()
ORDER BY subject_key
