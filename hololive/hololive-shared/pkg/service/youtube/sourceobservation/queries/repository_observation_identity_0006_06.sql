SELECT id,
       evidence_sha256
FROM source_observations
WHERE provider = $1
  AND observation_kind = $2
  AND subject_key = $3
  AND observation_key = $4
  AND schema_version = $5
  AND contract_generation = $6
FOR SHARE
