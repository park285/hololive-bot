SELECT COUNT(subject_key),
       COALESCE(MIN(poll_interval_ms), 0),
       COALESCE(MAX(poll_interval_ms), 0)
FROM youtube_collection_targets
WHERE projection_generation = $1
  AND observation_kind = ANY($2::text[])
  AND enabled = TRUE
  AND valid_until > clock_timestamp()
  AND (NOT $3 OR subject_key = $4)
