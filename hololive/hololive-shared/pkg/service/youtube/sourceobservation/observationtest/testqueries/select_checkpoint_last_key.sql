SELECT last_observation_key
FROM source_collection_checkpoints
WHERE provider = $1 AND observation_kind = $2 AND subject_key = $3 AND scope_sha256 = $4
