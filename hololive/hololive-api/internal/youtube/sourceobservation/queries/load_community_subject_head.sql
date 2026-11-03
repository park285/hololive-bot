SELECT source_observation_id,
       effective_at
FROM source_observation_subject_heads
WHERE provider = $1
  AND observation_kind = $2
  AND subject_key = $3
