SELECT observation_key,
       payload_sha256
FROM source_collection_checkpoints
WHERE source_kind = $1
  AND source_key = $2
FOR UPDATE
