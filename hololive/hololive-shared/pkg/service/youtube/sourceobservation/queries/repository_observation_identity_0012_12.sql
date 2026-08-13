SELECT id,
       payload_sha256
FROM source_observation_outbox
WHERE source_kind = $1
  AND source_key = $2
  AND observation_key = $3
  AND schema_version = $4
